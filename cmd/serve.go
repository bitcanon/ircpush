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
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	appcfg "github.com/bitcanon/ircpush/pkg/config"
	"github.com/bitcanon/ircpush/pkg/highlight"
	tcpin "github.com/bitcanon/ircpush/pkg/inputs/tcp"
	"github.com/bitcanon/ircpush/pkg/irc"
	"github.com/fsnotify/fsnotify"
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
			Connected: func() { fmt.Fprintln(os.Stderr, "irc: connected, joining channels...") },
			Welcome:   func(raw string) { fmt.Fprintf(os.Stderr, "<- %s\n", raw) },
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
		defer stop()

		// Create a logger that writes to stderr (captured by systemd)
		slog := log.New(os.Stderr, "", 0)

		srv := &tcpin.Server{
			ListenAddr:   cfg.TCP.Listen,
			IRC:          cli,
			HL:           hl,
			MaxLineBytes: 128 * 1024,
			Logger:       slog,
			// Only log per-message traffic when --debug or IRCPUSH_DEBUG=true
			LogMessages: viper.GetBool("debug"),
		}
		if err := srv.Start(ctx); err != nil {
			return err
		}

		// Reload handler updates runtime parts (currently: highlighting rules)
		reload := func(tag string) {
			var newCfg appcfg.Config
			if err := viper.Unmarshal(&newCfg); err != nil {
				fmt.Fprintf(os.Stderr, "reload: unmarshal failed: %v\n", err)
				return
			}
			// Hot-reload highlight rules
			srv.SetHighlighter(highlight.New(newCfg.Highlight))

			// Non-hot fields (inform user to restart if changed)
			if newCfg.TCP.Listen != cfg.TCP.Listen {
				fmt.Fprintf(os.Stderr, "reload: tcp.listen changed (%s -> %s), restart required\n", cfg.TCP.Listen, newCfg.TCP.Listen)
			}
			if newCfg.IRC.Server != cfg.IRC.Server || newCfg.IRC.Nick != cfg.IRC.Nick {
				fmt.Fprintf(os.Stderr, "reload: IRC connection settings changed, restart recommended\n")
			}
			cfg = newCfg
			fmt.Fprintf(os.Stderr, "reload: applied (%s)\n", tag)
		}

		// Optional: auto-reload via fsnotify when enabled
		if cfg.Highlight.AutoReload {
			viper.WatchConfig()
			viper.OnConfigChange(func(e fsnotify.Event) {
				fmt.Fprintf(os.Stderr, "config: change detected (%s)\n", e.Name)
				reload("fsnotify")
			})
			fmt.Fprintln(os.Stderr, "config: highlight auto-reload enabled")
		} else {
			fmt.Fprintln(os.Stderr, "config: highlight auto-reload disabled (use SIGHUP/systemctl reload)")
		}

		// Always support SIGHUP for manual reload
		hupCh := make(chan os.Signal, 1)
		signal.Notify(hupCh, syscall.SIGHUP)
		go func() {
			for range hupCh {
				fmt.Fprintln(os.Stderr, "signal: SIGHUP received, reloading config")
				reload("SIGHUP")
			}
		}()

		// Wait for termination
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
