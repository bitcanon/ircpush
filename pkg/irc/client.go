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
package irc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bitcanon/ircpush/pkg/config"
	"github.com/fluffle/goirc/client"
)

// Options configures client behaviors.
type Options struct {
	// DisableFlood keeps library throttling enabled unless set to false.
	// By default, flood protection is disabled (messages go out immediately).
	DisableFlood bool
	// Logger is where verbose/status logs can be written (optional).
	Logger io.Writer
}

// Handlers let callers receive status events (all optional).
type Handlers struct {
	Connected    func()
	Welcome      func(raw string)
	NickInUse    func(args []string)
	Joined       func(channel string)
	Notice       func(src, text string)
	Error        func(text string)
	Disconnected func()
}

// Client represents an IRC client with auto-reconnect and event handlers.
type Client struct {
	cfg      config.IRCConfig
	opts     Options
	handlers Handlers

	conn     *client.Conn
	ready    chan struct{} // closed once first "connected" fires
	stop     chan struct{} // closed to stop reconnect goroutine
	reconnCh chan struct{} // signal to (re)connect after disconnect
}

// New creates a new IRC client with the specified config, handlers, and options.
func New(cfg config.IRCConfig, h Handlers, o Options) (*Client, error) {
	if cfg.Server == "" || cfg.Nick == "" {
		return nil, fmt.Errorf("irc: server and nick are required")
	}

	ircCfg := client.NewConfig(cfg.Nick)
	ircCfg.SSL = cfg.TLS
	if cfg.ServerPass != "" {
		ircCfg.Pass = cfg.ServerPass
	}
	if cfg.Realname != "" {
		ircCfg.Me.Name = cfg.Realname
	}
	// Set ident to nick by default
	ircCfg.Me.Ident = cfg.Nick

	// Disable goirc throttling unless explicitly kept
	ircCfg.Flood = !o.DisableFlood

	if cfg.TLS {
		tlsCfg := &tls.Config{
			ServerName:         serverName(cfg.Server),
			InsecureSkipVerify: cfg.TLSSkipVerify,
			MinVersion:         tls.VersionTLS12,
		}
		// Client cert (optional)
		if cfg.TLSClientCert != "" && cfg.TLSClientKey != "" {
			if cert, err := tls.LoadX509KeyPair(cfg.TLSClientCert, cfg.TLSClientKey); err == nil {
				tlsCfg.Certificates = []tls.Certificate{cert}
			} else {
				// fallthrough; error will surface on connect if needed
				logf(o.Logger, "tls: load client cert failed: %v", err)
			}
		}
		// System CAs
		if pool, err := x509.SystemCertPool(); err == nil {
			tlsCfg.RootCAs = pool
		}
		ircCfg.SSLConfig = tlsCfg
	}

	c := &Client{
		cfg:      cfg,
		opts:     o,
		handlers: h,
		conn:     client.Client(ircCfg),
		ready:    make(chan struct{}),
		stop:     make(chan struct{}),
		reconnCh: make(chan struct{}, 1),
	}
	c.wireHandlers()
	return c, nil
}

// wireHandlers sets up internal event handlers for the IRC client.
func (c *Client) wireHandlers() {
	// First connection established
	c.conn.HandleFunc("connected", func(_ *client.Conn, _ *client.Line) {
		logf(c.opts.Logger, "irc: connected (tls=%v)", c.cfg.TLS)

		// NickServ identify (optional)
		if s := strings.TrimSpace(c.cfg.IdentifyPass); s != "" {
			logf(c.opts.Logger, "irc: identifying with NickServ")
			c.conn.Privmsg("NickServ", "IDENTIFY "+s)
			// Don't block; we'll see a notice when accepted
		}

		// Join channels (with keys when available)
		for _, ch := range c.cfg.Channels {
			ch = ensureChanPrefix(ch)
			if key := c.cfg.Keys[ch]; key != "" {
				logf(c.opts.Logger, "irc: join %s (with key)", ch)
				c.conn.Raw(fmt.Sprintf("JOIN %s %s", ch, key))
			} else {
				logf(c.opts.Logger, "irc: join %s", ch)
				c.conn.Join(ch)
			}
		}

		select {
		case <-c.ready:
		default:
			close(c.ready)
		}
		if c.handlers.Connected != nil {
			c.handlers.Connected()
		}
	})

	// The c.conn.HandleFunc below are other event handlers that
	// trigger the user-defined callbacks in c.handlers. This is
	// where we map IRC events to our client's event system.

	// Welcome numeric (001)
	c.conn.HandleFunc("001", func(_ *client.Conn, l *client.Line) {
		if c.handlers.Welcome != nil {
			c.handlers.Welcome(strings.TrimSpace(l.Raw))
		}
	})

	// Nick in use (433)
	c.conn.HandleFunc("433", func(_ *client.Conn, l *client.Line) {
		if c.handlers.NickInUse != nil {
			c.handlers.NickInUse(l.Args)
		}
	})

	// Our join confirmations
	c.conn.HandleFunc("join", func(conn *client.Conn, l *client.Line) {
		if l.Nick == conn.Me().Nick {
			ch := ""
			if len(l.Args) > 0 {
				ch = l.Args[0]
			}
			if c.handlers.Joined != nil {
				c.handlers.Joined(ch)
			}
		}
	})

	// Notices (NickServ/server)
	c.conn.HandleFunc("notice", func(_ *client.Conn, l *client.Line) {
		src := l.Nick
		if src == "" {
			src = "*"
		}
		txt := strings.TrimSpace(l.Text())
		if c.handlers.Notice != nil {
			c.handlers.Notice(src, txt)
		}
	})

	// Generic errors
	c.conn.HandleFunc("error", func(_ *client.Conn, l *client.Line) {
		msg := strings.TrimSpace(l.Raw)
		logf(c.opts.Logger, "irc error: %s", msg)
		if c.handlers.Error != nil {
			c.handlers.Error(msg)
		}
	})

	// Disconnected -> trigger reconnect
	c.conn.HandleFunc("disconnected", func(_ *client.Conn, _ *client.Line) {
		logf(c.opts.Logger, "irc: disconnected")
		if c.handlers.Disconnected != nil {
			c.handlers.Disconnected()
		}
		select {
		case c.reconnCh <- struct{}{}:
		default:
		}
	})
}

// Start connects and starts an auto-reconnect loop.
// It returns after the first successful connection or ctx timeout.
func (c *Client) Start(ctx context.Context) error {
	// Reconnect worker
	go c.reconnector()

	// Initial connect
	if err := c.conn.ConnectTo(c.cfg.Server); err != nil {
		return err
	}

	select {
	case <-c.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Broadcast sends msg to all configured channels.
func (c *Client) Broadcast(msg string) {
	for _, ch := range c.cfg.Channels {
		c.sendPrepared([]string{ch}, msg)
	}
}

func (c *Client) SendTo(channels []string, msg string) {
	c.sendPrepared(channels, msg)
}

// sendPrepared applies length policy (split/truncate) then sends each segment.
func (c *Client) sendPrepared(channels []string, msg string) {
	segs := c.segmentMessage(msg)
	for _, seg := range segs {
		for _, ch := range channels {
			if c.conn != nil {
				c.conn.Privmsg(ch, seg)
			}
		}
	}
}

// segmentMessage returns message segments according to MaxMessageLen/SplitLong.
func (c *Client) segmentMessage(msg string) []string {
	limit := c.cfg.MaxMessageLen
	// If no limit, return original message
	if limit <= 0 {
		return []string{msg}
	}

	// Check length and split/truncate as needed
	// The runes conversion handles multi-byte UTF-8 characters correctly.
	runes := []rune(msg)
	if len(runes) <= limit {
		return []string{msg}
	}

	// I SplitLong is false, truncate with "..." if possible
	if !c.cfg.SplitLong {
		// Check if we can append "..."
		if limit > 3 {
			return []string{string(runes[:limit-3]) + "..."}
		}
		// Just truncate without ellipsis
		return []string{string(runes[:limit])}
	}

	// II SplitLong is true, split into multiple segments
	var out []string
	start := 0
	for start < len(runes) {
		end := min(start+limit, len(runes))
		segment := runes[start:end]

		// Try to break on last space inside the segment (except for final segment).
		if end < len(runes) {
			if idx := lastSpace(segment); idx > 0 {
				end = start + idx
				segment = runes[start:end]
			}
		}

		out = append(out, string(segment))
		start = end
		// Skip leading space in next chunk to avoid segments starting with space.
		for start < len(runes) && runes[start] == ' ' {
			start++
		}
	}
	return out
}

func lastSpace(rs []rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i] == ' ' {
			return i
		}
	}
	return -1
}

// Quit asks the server to close the connection with a reason.
func (c *Client) Quit(reason string) {
	c.conn.Quit(reason)
}

// Close stops reconnect attempts (does not forcibly close the socket).
func (c *Client) Close() {
	select {
	case <-c.stop:
		// already closed
	default:
		close(c.stop)
	}
}

// reconnector handles automatic reconnections with exponential backoff.
func (c *Client) reconnector() {
	backoff := 1 * time.Second
	max := 30 * time.Second

	for {
		select {
		case <-c.stop:
			return
		case <-c.reconnCh:
			for {
				select {
				case <-c.stop:
					return
				default:
				}
				logf(c.opts.Logger, "irc: reconnecting in %s ...", backoff)
				time.Sleep(backoff)
				if err := c.conn.ConnectTo(c.cfg.Server); err != nil {
					logf(c.opts.Logger, "irc: reconnect failed: %v", err)
					if backoff < max {
						backoff *= 2
						if backoff > max {
							backoff = max
						}
					}
					continue
				}
				logf(c.opts.Logger, "irc: reconnect initiated")
				backoff = 1 * time.Second
				break
			}
		}
	}
}

// ensureChanPrefix makes sure the channel name starts with # or &.
func ensureChanPrefix(ch string) string {
	ch = strings.TrimSpace(ch)
	if ch == "" {
		return ch
	}
	if strings.HasPrefix(ch, "#") || strings.HasPrefix(ch, "&") {
		return ch
	}
	return "#" + ch
}

// serverName extracts the hostname from an address (host:port).
func serverName(addr string) string {
	if h, _, ok := strings.Cut(addr, ":"); ok {
		return h
	}
	return addr
}

// logf writes formatted logs to the provided writer (or stderr if nil).
func logf(w io.Writer, format string, a ...any) {
	if w == nil {
		// default to stderr if not provided
		w = os.Stderr
	}
	fmt.Fprintf(w, format+"\n", a...)
}
