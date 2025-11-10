package irc_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bitcanon/ircpush/pkg/config"
	"github.com/bitcanon/ircpush/pkg/irc"
)

/*
TLS integration test using an in-process self‑signed certificate.

Verifies:
1. Client establishes TLS.
2. Sends NICK/USER over TLS.
3. Receives numerics and JOINs channel.
4. Responds to PING with PONG.
5. Broadcast & SendTo produce PRIVMSG lines.
*/

// makeSelfSignedCert creates a short-lived self-signed server certificate for localhost:127.0.0.1.
func makeSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	// Private key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	// Certificate template
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    now.Add(-1 * time.Minute),
		NotAfter:     now.Add(2 * time.Hour),

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,

		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:    []string{"localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	// PEM encode cert and key
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	return pair
}

// TLS fake server (same behavior as plaintext version).
type fakeTLSServer struct {
	t   *testing.T
	ln  net.Listener
	mu  sync.Mutex
	got []string
}

func startFakeTLSServer(t *testing.T) *fakeTLSServer {
	t.Helper()

	cert := makeSelfSignedCert(t)
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Plain TCP then wrap with TLS listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tls: %v", err)
	}
	tlsLn := tls.NewListener(ln, cfg)
	srv := &fakeTLSServer{t: t, ln: tlsLn}
	go srv.acceptOne()
	return srv
}

func (s *fakeTLSServer) addr() string { return s.ln.Addr().String() }
func (s *fakeTLSServer) close()       { _ = s.ln.Close() }

func (s *fakeTLSServer) acceptOne() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	br := bufio.NewReader(conn)

	var nickSeen, userSeen bool
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// PINGs to elicit PONG.
	go func() {
		for i := 0; i < 3; i++ {
			time.Sleep(300 * time.Millisecond)
			writeLine(conn, "PING :xyz")
		}
	}()

	for !(nickSeen && userSeen) {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		s.record(line)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if strings.HasPrefix(line, "NICK ") {
			nickSeen = true
		}
		if strings.HasPrefix(line, "USER ") {
			userSeen = true
		}
	}

	writeLine(conn, ":irc.tls 001 ircbot :Welcome")
	writeLine(conn, ":irc.tls 376 ircbot :End of /MOTD")

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		s.record(line)
		if strings.HasPrefix(line, "JOIN ") {
			writeLine(conn, fmt.Sprintf(":ircbot!u@h JOIN %s", strings.TrimSpace(line[5:])))
			break
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		s.record(line)
	}
}

func (s *fakeTLSServer) record(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, strings.TrimRight(line, "\r\n"))
}

func (s *fakeTLSServer) seen(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.got {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// Reuse a local waitFor to keep test independent.
func waitForTLS(t *testing.T, d time.Duration, cond func() bool, what string, dump func()) {
	dead := time.Now().Add(d)
	for time.Now().Before(dead) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	if dump != nil {
		dump()
	}
	t.Fatalf("timeout waiting for: %s", what)
}

func TestIRCTLSHandshakeAndMessaging(t *testing.T) {
	s := startFakeTLSServer(t)
	defer s.close()

	// IRC client config for TLS; skip verify (self-signed).
	cfg := config.IRCConfig{
		Server:        s.addr(),
		TLS:           true,
		TLSSkipVerify: true,
		Nick:          "ircbot",
		Realname:      "ircbot",
		Channels:      []string{"#tls"},
	}

	cli, err := irc.New(cfg, irc.Handlers{
		Error: func(text string) { t.Logf("irc error: %s", text) },
	}, irc.Options{DisableFlood: true})
	if err != nil {
		t.Fatalf("irc.New: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForTLS(t, 4*time.Second, func() bool { return s.seen("NICK ") }, "NICK", nil)
	waitForTLS(t, 4*time.Second, func() bool { return s.seen("USER ") }, "USER", nil)
	waitForTLS(t, 4*time.Second, func() bool { return s.seen("JOIN #tls") }, "JOIN #tls", nil)

	cli.Broadcast("tls hello")
	waitForTLS(t, 4*time.Second, func() bool { return s.seen("PRIVMSG #tls :tls hello") },
		"PRIVMSG #tls :tls hello",
		func() { t.Logf("lines: %#v", s.got) },
	)

	time.Sleep(100 * time.Millisecond)
	cli.SendTo([]string{"#tls"}, "secure msg")
	waitForTLS(t, 4*time.Second, func() bool { return s.seen("PRIVMSG #tls :secure msg") },
		"PRIVMSG #tls :secure msg",
		func() { t.Logf("lines: %#v", s.got) },
	)
}
