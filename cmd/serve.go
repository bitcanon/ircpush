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
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	appcfg "github.com/bitcanon/ircpush/pkg/config"
	"github.com/bitcanon/ircpush/pkg/highlight"
	tcpin "github.com/bitcanon/ircpush/pkg/inputs/tcp"
	"github.com/bitcanon/ircpush/pkg/irc"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run TCP listener and forward incoming lines to IRC",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config via Viper (initConfig in root initializes Viper)
		var cfg appcfg.Config
		if err := viper.Unmarshal(&cfg); err != nil {
			return fmt.Errorf("unmarshal config: %w", err)
		}
		if cfg.IRC.Server == "" || cfg.IRC.Nick == "" {
			return fmt.Errorf("irc.server and irc.nick must be set in config")
		}
		if len(cfg.IRC.Channels) == 0 {
			return fmt.Errorf("irc.channels must contain at least one channel")
		}
		if cfg.TCP.Listen == "" {
			return fmt.Errorf("tcp.listen must be set (e.g. 10.20.30.40:9000 or :9000)")
		}

		// Verbose summary
		fmt.Fprintf(os.Stderr, "IRC server: %s\n", cfg.IRC.Server)
		if cfg.IRC.TLS {
			fmt.Fprintf(os.Stderr, "TLS: enabled (skip_verify=%v)\n", cfg.IRC.TLSSkipVerify)
		} else {
			fmt.Fprintln(os.Stderr, "TLS: disabled")
		}
		fmt.Fprintf(os.Stderr, "Nick: %s, Channels: %s\n", cfg.IRC.Nick, strings.Join(cfg.IRC.Channels, ", "))
		fmt.Fprintf(os.Stderr, "TCP listen: %s\n", cfg.TCP.Listen)

		// Build IRC client
		cli, err := irc.New(cfg.IRC, irc.Handlers{
			Connected: func() {
				fmt.Fprintln(os.Stderr, "irc: connected, joining channels...")
			},
			Welcome: func(raw string) {
				fmt.Fprintf(os.Stderr, "<- %s\n", raw)
			},
			Disconnected: func() {
				fmt.Fprintln(os.Stderr, "irc: disconnected (will auto-reconnect)")
			},
			Error: func(text string) {
				fmt.Fprintf(os.Stderr, "irc error: %s\n", text)
			},
		}, irc.Options{
			DisableFlood: false,
			Logger:       os.Stderr,
		})
		if err != nil {
			return err
		}
		defer cli.Close()

		// Start IRC connection with timeout
		ictx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := cli.Start(ictx); err != nil {
			return fmt.Errorf("irc connect: %w", err)
		}
		fmt.Fprintln(os.Stderr, "irc: ready")

		// Highlighter from config
		hl := highlight.New(cfg.Highlight)

		// Start TCP server
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop() // stop signal handling on exit

		// TCP server that forwards to IRC client
		srv := &tcpin.Server{
			ListenAddr:   cfg.TCP.Listen,
			IRC:          cli,
			HL:           hl,
			MaxLineBytes: 128 * 1024, // allow fairly large lines
		}
		if err := srv.Start(ctx); err != nil {
			return err
		}

		// Wait for signal
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "shutting down...")
		_ = srv.Stop()
		cli.Quit("shutdown")
		time.Sleep(200 * time.Millisecond)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
