/*
MIT License

Copyright (c) 2025 Mikael Schultz <mikael@conf-t.se>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
package tcp

import (
	"bufio"
	"context"
	"errors" // added
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bitcanon/ircpush/pkg/highlight"
	"github.com/bitcanon/ircpush/pkg/irc"
)

// Server receives messages over TCP and forwards them to IRC.
type Server struct {
	ListenAddr   string
	IRC          *irc.Client

	// Highlighter can be swapped at runtime via SetHighlighter.
	mu sync.RWMutex
	HL *highlight.Highlighter

	// Optional logging sink; if nil, logs go to stderr.
	Logger Logger

	// Control whether to log each received message (default false).
	LogMessages bool

	// Scanner limits
	MaxLineBytes int

	ln   net.Listener
	wg   sync.WaitGroup
	once sync.Once
}

// Logger is a minimal logger interface.
type Logger interface {
	Printf(format string, v ...any)
}

func (s *Server) logf(format string, v ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, v...)
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", v...)
}

// Start begins listening and serving connections until ctx is done or an error occurs.
// It returns once the listener is up and the accept loop has been started.
// Use Stop() to close the listener.
func (s *Server) Start(ctx context.Context) error {
	if s.ListenAddr == "" {
		return fmt.Errorf("tcp server: ListenAddr is empty")
	}
	if s.IRC == nil {
		return fmt.Errorf("tcp server: IRC client is nil")
	}
	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.ListenAddr, err)
	}
	s.ln = ln
	s.logf("tcp: listening on %s", s.ListenAddr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				// Listener likely closed during shutdown.
				if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
					s.logf("tcp: listener closed")
					return
				}
				// Treat timeouts as transient; retry after a short delay.
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					s.logf("tcp: accept timeout: %v", err)
					time.Sleep(100 * time.Millisecond)
					continue
				}
				// Non-timeout error: log and stop the accept loop.
				s.logf("tcp: accept error: %v", err)
				return
			}
			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				_ = c.SetDeadline(time.Time{}) // clear deadline
				_ = c.SetReadDeadline(time.Time{})
				s.handleConn(ctx, c)
			}(conn)
		}
	}()

	// Close listener when ctx is done
	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	return nil
}

// Stop closes the listener and waits for connection handlers to finish.
func (s *Server) Stop() error {
	var err error
	s.once.Do(func() {
		if s.ln != nil {
			err = s.ln.Close()
		}
	})
	// Give active goroutines a short grace period to finish reading current lines.
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	return err
}

func (s *Server) handleConn(ctx context.Context, c net.Conn) {
	ra := c.RemoteAddr().String()
	s.logf("tcp: connection from %s", ra)
	defer func() {
		_ = c.Close()
		s.logf("tcp: closed %s", ra)
	}()

	sc := bufio.NewScanner(c)
	// Increase max line size if requested
	if s.MaxLineBytes <= 0 {
		s.MaxLineBytes = 64 * 1024
	}
	buf := make([]byte, 0, 16*1024)
	sc.Buffer(buf, s.MaxLineBytes)

	for sc.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := strings.TrimRight(sc.Text(), "\r\n")
		if line == "" {
			continue
		}

		// Parse optional leading channels (e.g. "#server msg" or "#a,#b msg")
		targets, msg := parseTargets(line)
		if len(targets) == 0 {
			if s.LogMessages {
				s.logf("tcp: %s -> broadcast: %q", ra, line)
			}
			s.broadcast(line)
			continue
		}

		// Send only to specified channels
		if strings.TrimSpace(msg) == "" {
			// If there's no message after the channels, skip
			if s.LogMessages {
				s.logf("tcp: %s -> empty message after targets %v", ra, targets)
			}
			continue
		}
		if s.LogMessages {
			s.logf("tcp: %s -> targets %v: %q", ra, targets, msg)
		}
		for _, ch := range targets {
			colored := s.applyHL(ch, msg)
			s.IRC.SendTo([]string{ch}, colored)
		}
	}
	if err := sc.Err(); err != nil {
		s.logf("tcp: %s scanner error: %v", ra, err)
	}
}

func (s *Server) applyHL(channel, msg string) string {
	s.mu.RLock()
	hl := s.HL
	s.mu.RUnlock()
	if hl == nil {
		return msg
	}
	return hl.ApplyFor(channel, msg)
}

// SetHighlighter replaces the active highlighter safely at runtime.
func (s *Server) SetHighlighter(h *highlight.Highlighter) {
	s.mu.Lock()
	s.HL = h
	s.mu.Unlock()
	s.logf("tcp: highlighter reloaded")
}

func (s *Server) broadcast(line string) {
	// We don't know the configured channel list here. The IRC client has it.
	// Broadcast and let the client expand channels.
	colored := s.applyHL("", line)
	s.IRC.Broadcast(colored)
}

// parseTargets parses an optional leading channel list and returns targets + message.
// Examples:
//
//	"#security hello"    -> ["#security"], "hello"
//	"#a,#b hi"           -> ["#a", "#b"], "hi"
//	"no prefix"          -> nil, "no prefix"
func parseTargets(line string) ([]string, string) {
	s := strings.TrimSpace(line)
	if s == "" {
		return nil, ""
	}
	if !(strings.HasPrefix(s, "#") || strings.HasPrefix(s, "&")) {
		return nil, s
	}
	first, rest, hasRest := strings.Cut(s, " ")
	chTokens := strings.Split(first, ",")

	var out []string
	seen := map[string]struct{}{}
	for _, ch := range chTokens {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}
		if !strings.HasPrefix(ch, "#") && !strings.HasPrefix(ch, "&") {
			ch = "#" + ch
		}
		lc := strings.ToLower(ch)
		if _, ok := seen[lc]; ok {
			continue
		}
		seen[lc] = struct{}{}
		out = append(out, ch)
	}

	msg := ""
	if hasRest {
		msg = strings.TrimSpace(rest)
	}
	return out, msg
}
