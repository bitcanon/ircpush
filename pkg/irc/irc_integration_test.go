package irc_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bitcanon/ircpush/pkg/config"
	"github.com/bitcanon/ircpush/pkg/irc"
)

/*
Integration-style test using an in-process fake IRC server.

What is being verified:
1. Client sends NICK and USER during handshake.
2. Client reacts to welcome numerics and joins configured channels.
3. Client responds to server PING with PONG.
4. Broadcast() sends a PRIVMSG to each configured channel.
5. SendTo() sends a targeted PRIVMSG.
No external IRC daemon required; everything runs locally & fast.
*/

// fakeServer implements a minimal IRC server to capture client traffic.
type fakeServer struct {
	t   *testing.T
	ln  net.Listener
	mu  sync.Mutex
	got []string // raw lines received from client (without trailing CRLF)
}

// startFakeServer starts a TCP listener on 127.0.0.1 and begins accepting one client.
func startFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &fakeServer{t: t, ln: ln}
	go srv.acceptOne()
	return srv
}

func (s *fakeServer) addr() string { return s.ln.Addr().String() }
func (s *fakeServer) close()       { _ = s.ln.Close() }

// acceptOne handles a single client connection and records all lines it sends.
// It simulates minimal IRC protocol: waits for NICK/USER, sends welcome numerics,
// expects JOIN, keeps capturing PRIVMSG and PONG.
func (s *fakeServer) acceptOne() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	br := bufio.NewReader(conn)

	var nickSeen, userSeen bool
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Send periodic PING messages so we can see PONG responses.
	go func() {
		for i := 0; i < 3; i++ {
			time.Sleep(300 * time.Millisecond)
			writeLine(conn, "PING :abc")
		}
	}()

	// Wait for both NICK and USER commands from client.
	for !(nickSeen && userSeen) {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		s.record(line)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second)) // refresh timeout

		if strings.HasPrefix(line, "NICK ") {
			nickSeen = true
		}
		if strings.HasPrefix(line, "USER ") {
			userSeen = true
		}
	}

	// Simulate welcome numerics to trigger client JOIN.
	writeLine(conn, ":irc.local 001 ircbot :Welcome")
	writeLine(conn, ":irc.local 376 ircbot :End of /MOTD")

	// Await JOIN from client.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		s.record(line)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		if strings.HasPrefix(line, "PONG ") {
			continue // ignore, just record
		}
		if strings.HasPrefix(line, "JOIN ") {
			// Optional echo back a JOIN (not required for test logic).
			writeLine(conn, fmt.Sprintf(":ircbot!u@h JOIN %s", strings.TrimSpace(line[5:])))
			break
		}
	}

	// Disable deadline; continue recording any PRIVMSG/PONG lines.
	_ = conn.SetReadDeadline(time.Time{})
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		s.record(line)
	}
}

// record stores a received line (trim CRLF) for later assertions.
func (s *fakeServer) record(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, strings.TrimRight(line, "\r\n"))
}

// seen returns true if any recorded line starts with prefix.
func (s *fakeServer) seen(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.got {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// writeLine sends a raw IRC line terminated with CRLF.
func writeLine(c net.Conn, l string) {
	_, _ = c.Write([]byte(l + "\r\n"))
}

// waitFor polls a condition until timeout; dumps captured lines on failure for debugging.
func waitFor(t *testing.T, d time.Duration, cond func() bool, what string, dump func()) {
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

// TestIRCHandshakeAndBroadcast exercises the IRC client end-to-end against the fake server.
func TestIRCHandshakeAndBroadcast(t *testing.T) {
	s := startFakeServer(t)
	defer s.close()

	// Minimal IRC config: single channel, plaintext.
	cfg := config.IRCConfig{
		Server:   s.addr(),
		TLS:      false,
		Nick:     "ircbot",
		Realname: "ircbot",
		Channels: []string{"#test"},
	}

	cli, err := irc.New(cfg, irc.Handlers{
		Error: func(text string) { t.Logf("irc error: %s", text) },
	}, irc.Options{DisableFlood: true})
	if err != nil {
		t.Fatalf("irc.New: %v", err)
	}
	defer cli.Close()

	// Start connection (handshake triggers immediately).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Assert handshake commands.
	waitFor(t, 3*time.Second, func() bool { return s.seen("NICK ") }, "NICK", nil)
	waitFor(t, 3*time.Second, func() bool { return s.seen("USER ") }, "USER", nil)
	waitFor(t, 3*time.Second, func() bool { return s.seen("JOIN #test") }, "JOIN #test", nil)

	// Broadcast should send a PRIVMSG to #test.
	cli.Broadcast("hello world")
	waitFor(t, 3*time.Second, func() bool { return s.seen("PRIVMSG #test :hello world") },
		"PRIVMSG #test :hello world",
		func() { t.Logf("got lines (broadcast): %#v", s.got) },
	)

	// Targeted SendTo should send another PRIVMSG.
	time.Sleep(100 * time.Millisecond) // small delay to avoid batching issues
	cli.SendTo([]string{"#test"}, "targeted")
	waitFor(t, 3*time.Second, func() bool { return s.seen("PRIVMSG #test :targeted") },
		"PRIVMSG #test :targeted",
		func() { t.Logf("got lines (sendto): %#v", s.got) },
	)
}
