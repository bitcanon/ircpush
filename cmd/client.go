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
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bitcanon/ircpush/pkg/config"
	"github.com/bitcanon/ircpush/pkg/highlight"
	"github.com/bitcanon/ircpush/pkg/irc"
)

// Example help text for the client command
var clientExample = `  ircpush client`

// clientCmd represents the client command
var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Send text messages to IRC channels interactively",
	Long: `Start an interactive IRC client that sends text messages to configured IRC channels.
Append '#channel' prefix to messages to target specific channels. Use /quit to exit.`,
	Example:      clientExample,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config
		var cfg config.Config
		if err := viper.Unmarshal(&cfg); err != nil {
			return fmt.Errorf("unmarshal config: %w", err)
		}
		if cfg.IRC.Server == "" || cfg.IRC.Nick == "" {
			return fmt.Errorf("config missing irc.server or irc.nick")
		}
		if len(cfg.IRC.Channels) == 0 {
			return fmt.Errorf("config irc.channels cannot be empty")
		}

		// Verbose summary
		fmt.Fprintf(os.Stderr, "IRC server: %s\n", cfg.IRC.Server)
		if cfg.IRC.TLS {
			fmt.Fprintf(os.Stderr, "TLS: enabled (skip_verify=%v, client_cert=%t)\n",
				cfg.IRC.TLSSkipVerify,
				cfg.IRC.TLSClientCert != "" && cfg.IRC.TLSClientKey != "")
		} else {
			fmt.Fprintln(os.Stderr, "TLS: disabled")
		}
		fmt.Fprintf(os.Stderr, "Nick: %s, Realname: %s\n", cfg.IRC.Nick, cfg.IRC.Realname)
		fmt.Fprintf(os.Stderr, "Channels: %s\n", strings.Join(cfg.IRC.Channels, ", "))

		// Build IRC client with handlers and options
		cli, err := irc.New(cfg.IRC, irc.Handlers{
			Connected: func() {
				printPrompt(os.Stdout, cfg.IRC.Nick)
			},
			Welcome: func(raw string) {
				fmt.Fprintf(os.Stderr, "<- %s\n", raw)
				printPrompt(os.Stdout, cfg.IRC.Nick)
			},
			NickInUse: func(args []string) {
				fmt.Fprintf(os.Stderr, "WARN: Nick already in use (%s). Args=%v\n", cfg.IRC.Nick, args)
				printPrompt(os.Stdout, cfg.IRC.Nick)
			},
			Joined: func(channel string) {
				fmt.Fprintf(os.Stderr, "Joined: %s\n", channel)
				printPrompt(os.Stdout, cfg.IRC.Nick)
			},
			Notice: func(src, text string) {
				fmt.Fprintf(os.Stderr, "[NOTICE] %s: %s\n", src, text)
				printPrompt(os.Stdout, cfg.IRC.Nick)
			},
			Error: func(text string) {
				fmt.Fprintf(os.Stderr, "irc error: %s\n", text)
				printPrompt(os.Stdout, cfg.IRC.Nick)
			},
			Disconnected: func() {
				fmt.Fprintln(os.Stderr, "Disconnected. Reconnecting will be attempted...")
				printPrompt(os.Stdout, cfg.IRC.Nick)
			},
		}, irc.Options{
			DisableFlood: false,     // send without client throttling
			Logger:       os.Stderr, // verbose logs
		})
		if err != nil {
			return err
		}
		defer cli.Close()

		// Initial connect with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := cli.Start(ctx); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Ready. Joined channels: %s\n", strings.Join(cfg.IRC.Channels, ", "))

		// Create highlighter from config
		hl := highlight.New(cfg.Highlight)

		// Interactive send loop
		fmt.Println("Type messages. Prefix with #channel to target it (e.g. '#security hello'). Use /quit to exit.")
		printPrompt(os.Stdout, cfg.IRC.Nick)
		sc := bufio.NewScanner(os.Stdin)
		for {
			if !sc.Scan() {
				break
			}
			line := strings.TrimRight(sc.Text(), "\r\n")
			if strings.EqualFold(line, "/quit") {
				break
			}
			if strings.TrimSpace(line) == "" {
				printPrompt(os.Stdout, cfg.IRC.Nick)
				continue
			}

			// Parse optional channel prefix
			targets, msg := parseTargets(line, cfg.IRC.Channels)

			if len(targets) == 0 {
				// Broadcast to all joined channels with channel-aware highlighting
				for _, ch := range cfg.IRC.Channels {
					ch = ensureChanPrefix(ch)
					col := hl.ApplyFor(ch, line)
					fmt.Fprintf(os.Stderr, "-> PRIVMSG %s: %s\n", ch, line)
					cli.SendTo([]string{ch}, col)
				}
			} else {
				// Targeted send with channel-aware highlighting
				for _, ch := range targets {
					ch = ensureChanPrefix(ch)
					col := hl.ApplyFor(ch, msg)
					fmt.Fprintf(os.Stderr, "-> PRIVMSG %s: %s\n", ch, msg)
					cli.SendTo([]string{ch}, col)
				}
			}

			printPrompt(os.Stdout, cfg.IRC.Nick)
		}
		if err := sc.Err(); err != nil {
			return err
		}

		fmt.Fprintln(os.Stderr, "Quitting...")
		cli.Quit("bye")
		time.Sleep(300 * time.Millisecond)
		return nil
	},
}

// init registers the client command
func init() {
	rootCmd.AddCommand(clientCmd)

	// Here you will define your flags and configuration settings.

	// You can define flags and configuration settings specific to this command here.
}

// ensureChanPrefix ensures that the channel name starts with '#' or '&'
func ensureChanPrefix(ch string) string {
	ch = strings.TrimSpace(ch)
	if ch == "" || strings.HasPrefix(ch, "#") || strings.HasPrefix(ch, "&") {
		return ch
	}
	return "#" + ch
}

// printPrompt writes the interactive prompt, e.g. "[ircbot] "
func printPrompt(out io.Writer, nick string) {
	fmt.Fprintf(out, "[%s] ", nick)
}

// parseTargets parses an optional leading channel list and returns targets + message.
// Examples:
//
//	"#security hello"      -> targets: ["#security"], msg: "hello"
//	"#a,#b hi"             -> targets: ["#a", "#b"], msg: "hi"
//	"no prefix"            -> targets: nil, msg: "no prefix" (broadcast)
func parseTargets(line string, joined []string) ([]string, string) {
	s := strings.TrimSpace(line)
	if s == "" {
		return nil, ""
	}
	if !(strings.HasPrefix(s, "#") || strings.HasPrefix(s, "&")) {
		return nil, s
	}

	// Split first token (channels) from the rest (message)
	first, rest, hasRest := strings.Cut(s, " ")
	chTokens := strings.Split(first, ",")

	// Build a set of joined channels to filter against
	joinedSet := make(map[string]struct{}, len(joined))
	for _, jc := range joined {
		joinedSet[strings.ToLower(ensureChanPrefix(jc))] = struct{}{}
	}

	var targets []string
	for _, ch := range chTokens {
		ch = ensureChanPrefix(strings.TrimSpace(ch))
		if ch == "" {
			continue
		}
		// Only allow sending to channels we joined
		if _, ok := joinedSet[strings.ToLower(ch)]; ok {
			targets = append(targets, ch)
		}
	}

	msg := ""
	if hasRest {
		msg = strings.TrimSpace(rest)
	}
	return targets, msg
}
