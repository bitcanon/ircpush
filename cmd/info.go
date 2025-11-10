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
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	appcfg "github.com/bitcanon/ircpush/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var infoCmd = &cobra.Command{
	Use:          "info",
	Short:        "Show effective configuration and overrides",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1) Which config file was used?
		cf := viper.ConfigFileUsed()
		if cf == "" {
			fmt.Fprintln(os.Stderr, "Config file: (none found via search order)")
		} else {
			fmt.Fprintf(os.Stderr, "Config file: %s\n", cf)
		}

		// 2) Capture effective config (file + env + flags)
		var effective appcfg.Config
		if err := viper.Unmarshal(&effective); err != nil {
			return fmt.Errorf("unmarshal effective config: %w", err)
		}

		// 3) Load file-only config (to diff against) if a file was used
		var fileOnly appcfg.Config
		if cf != "" {
			raw, err := os.ReadFile(cf)
			if err != nil {
				return fmt.Errorf("read config file: %w", err)
			}
			if err := yaml.Unmarshal(raw, &fileOnly); err != nil {
				return fmt.Errorf("parse config file: %w", err)
			}
		}

		// 4) List environment overrides (IRCPUSH_*)
		envVars := collectEnv("IRCPUSH_")
		if len(envVars) > 0 {
			fmt.Fprintln(os.Stderr, "Environment overrides (present):")
			for _, kv := range envVars {
				fmt.Fprintf(os.Stderr, "  %s\n", kv)
			}
		} else {
			fmt.Fprintln(os.Stderr, "Environment overrides: (none)")
		}

		// 5) Print effective config as YAML
		fmt.Fprintln(os.Stderr, "\nEffective config:")
		ymlEff, _ := yaml.Marshal(effective)
		os.Stderr.Write(ymlEff)

		// 6) Print overrides (effective vs file-only)
		if cf != "" {
			overrides := diffOverrides(fileOnly, effective)
			if len(overrides) == 0 {
				fmt.Fprintln(os.Stderr, "\nOverrides: (none; effective == file)")
			} else {
				fmt.Fprintln(os.Stderr, "\nOverrides (file -> effective):")
				for _, o := range overrides {
					// Try to hint if an env var likely set this key
					envKey := "IRCPUSH_" + strings.ToUpper(strings.ReplaceAll(o.Key, ".", "_"))
					src := ""
					if hasEnv(envKey) {
						src = " [env]"
					}
					fmt.Fprintf(os.Stderr, "  %s: %s -> %s%s\n", o.Key, o.Old, o.New, src)
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "\nOverrides: (no config file in use; everything is from env/flags/defaults)")
		}

		// 7) Show a couple of notable flags that can override behavior
		fmt.Fprintf(os.Stderr, "\nFlags:\n  debug: %v (from --debug or IRCPUSH_DEBUG)\n", viper.GetBool("debug"))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}

// collectEnv returns sorted KEY=VALUE lines for vars starting with prefix.
func collectEnv(prefix string) []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	sort.Strings(out)
	return out
}

func hasEnv(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}

// diffOverrides compares file-only config vs effective config, returns flattened diffs.
type override struct {
	Key string
	Old string
	New string
}

func diffOverrides(fileCfg, effCfg any) []override {
	fm := mustStructToMap(fileCfg)
	em := mustStructToMap(effCfg)

	ff := flattenMap("", fm)
	ef := flattenMap("", em)

	var keys []string
	seen := map[string]struct{}{}
	for k := range ef {
		seen[k] = struct{}{}
	}
	for k := range ff {
		seen[k] = struct{}{}
	}
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []override
	for _, k := range keys {
		oldV, oldOK := ff[k]
		newV, newOK := ef[k]
		// normalize to strings
		oldS := valueString(oldV)
		newS := valueString(newV)

		// Only report when both sides exist but differ,
		// or when key exists only on one side and non-empty.
		if oldOK && newOK {
			if oldS != newS {
				out = append(out, override{Key: k, Old: oldS, New: newS})
			}
		} else if !oldOK && newOK {
			out = append(out, override{Key: k, Old: "(unset in file)", New: newS})
		} else if oldOK && !newOK {
			out = append(out, override{Key: k, Old: oldS, New: "(unset effective)"})
		}
	}
	return out
}

func mustStructToMap(v any) map[string]any {
	// Marshal to YAML then unmarshal into map[string]any to get a generic map
	b, _ := yaml.Marshal(v)
	var m map[string]any
	_ = yaml.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// flattenMap flattens nested maps into dot-keys: "irc.server", "tcp.listen", etc.
func flattenMap(prefix string, m map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			for fk, fv := range flattenMap(key, t) {
				out[fk] = fv
			}
		case []any:
			// Represent slices as YAML strings to get readable diffs
			var buf bytes.Buffer
			enc := yaml.NewEncoder(&buf)
			_ = enc.Encode(t)
			_ = enc.Close()
			out[key] = strings.TrimSpace(buf.String())
		default:
			out[key] = t
		}
	}
	return out
}

func valueString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		b, _ := yaml.Marshal(x)
		return strings.TrimSpace(string(b))
	}
}
