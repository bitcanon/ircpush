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
	Short: "Run TCP listener and forward incoming messages to IRC",
	Long: `Run TCP listener and forward incoming messages to IRC.

The serve command starts the main ircpush service, which listens for incoming
text messages via a TCP listener and forwards them to configured IRC channels
after applying powerful regex syntax highlighting rules.

Message -> TCP listener -> Highlighting -> IRC channels

The command loads its configuration from the config file (default: ./config.yaml)
and supports hot-reloading of the highlighting rules when the config file changes
(if enabled in config) or when receiving a SIGHUP signal.`,
	SilenceUsage: true, // avoid printing usage on errors
	RunE: func(cmd *cobra.Command, args []string) error {
		if cf := viper.ConfigFileUsed(); cf != "" {
			fmt.Fprintf(os.Stderr, "Config file: %s\n", cf)
		}
		var cfg appcfg.Config
		if err := viper.Unmarshal(&cfg); err != nil {
			return fmt.Errorf("unmarshal config: %w", err)
		}
		if cfg.TCP.MaxLineBytes == 0 {
			cfg.TCP.MaxLineBytes = 64 * 1024
		}
		// Print effective settings to catch env overrides
		fmt.Fprintf(os.Stderr, "IRC server: %s\n", cfg.IRC.Server)
		fmt.Fprintf(os.Stderr, "TLS: %v (skip_verify=%v)\n", cfg.IRC.TLS, cfg.IRC.TLSSkipVerify)
		fmt.Fprintf(os.Stderr, "Nick: %s, Channels: %s\n", cfg.IRC.Nick, strings.Join(cfg.IRC.Channels, ", "))
		fmt.Fprintf(os.Stderr, "TCP listen: %s\n", cfg.TCP.Listen)
		fmt.Fprintf(os.Stderr, "IRC msg policy: max_len=%d split_long=%v\n", cfg.IRC.MaxMessageLen, cfg.IRC.SplitLong)
		fmt.Fprintf(os.Stderr, "TCP max_line_bytes: %d (0=default 65536)\n", cfg.TCP.MaxLineBytes)

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
			MaxLineBytes: cfg.TCP.MaxLineBytes, // new: honor tcp.max_line_bytes
			Logger:       slog,
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
