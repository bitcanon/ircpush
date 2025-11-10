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
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// TCPConfig holds TCP listener settings.
type TCPConfig struct {
	Listen       string `yaml:"listen"          mapstructure:"listen"`
	MaxLineBytes int    `yaml:"max_line_bytes"  mapstructure:"max_line_bytes"` // 0 => default 65536
}

type IRCConfig struct {
	Server        string            `yaml:"server"          mapstructure:"server"`
	TLS           bool              `yaml:"tls"             mapstructure:"tls"`
	TLSSkipVerify bool              `yaml:"tls_skip_verify" mapstructure:"tls_skip_verify"`
	TLSClientCert string            `yaml:"tls_client_cert" mapstructure:"tls_client_cert"`
	TLSClientKey  string            `yaml:"tls_client_key"  mapstructure:"tls_client_key"`
	Nick          string            `yaml:"nick"            mapstructure:"nick"`
	Realname      string            `yaml:"realname"        mapstructure:"realname"`
	ServerPass    string            `yaml:"server_pass"     mapstructure:"server_pass"`
	IdentifyPass  string            `yaml:"identify_pass"   mapstructure:"identify_pass"`
	SASLExternal  bool              `yaml:"sasl_external"   mapstructure:"sasl_external"`
	SASLLogin     string            `yaml:"sasl_login"      mapstructure:"sasl_login"`
	SASLPass      string            `yaml:"sasl_pass"       mapstructure:"sasl_pass"`
	Channels      []string          `yaml:"channels"        mapstructure:"channels"`
	Keys          map[string]string `yaml:"keys"   mapstructure:"keys"`

	// New: maximum length of an IRC message payload after highlighting (characters). 0 = unlimited.
	MaxMessageLen int `yaml:"max_message_len" mapstructure:"max_message_len"`
	// New: if true, split messages longer than MaxMessageLen into multiple PRIVMSGs;
	// if false, truncate and append "..." (only when MaxMessageLen > 3).
	SplitLong bool `yaml:"split_long" mapstructure:"split_long"`
}

type HighlightConfig struct {
	Rules      []HighlightRule `yaml:"rules"       mapstructure:"rules"`
	AutoReload bool            `yaml:"auto_reload" mapstructure:"auto_reload"` // watch file and auto-reload rules
}

type HighlightRule struct {
	Kind            string   `yaml:"kind"               mapstructure:"kind"`
	Pattern         string   `yaml:"pattern"            mapstructure:"pattern"`
	Color           string   `yaml:"color"              mapstructure:"color"`
	Bold            bool     `yaml:"bold"               mapstructure:"bold"`
	Underline       bool     `yaml:"underline"          mapstructure:"underline"`
	CaseInsensitive bool     `yaml:"case_insensitive"   mapstructure:"case_insensitive"`
	WholeLine       bool     `yaml:"whole_line"         mapstructure:"whole_line"`
	Channels        []string `yaml:"channels"           mapstructure:"channels"`
	ExcludeChannels []string `yaml:"exclude_channels"   mapstructure:"exclude_channels"`

	// New: color only these submatch groups (by index or name). Example: ["1","2"] or ["src","dst"]
	Groups []string `yaml:"groups"              mapstructure:"groups"`
}

// Config is the root application config.
type Config struct {
	IRC       IRCConfig       `yaml:"irc"        mapstructure:"irc"`
	TCP       TCPConfig       `yaml:"tcp"        mapstructure:"tcp"`
	Highlight HighlightConfig `yaml:"highlight"  mapstructure:"highlight"`
}

// Optional: legacy direct YAML loader (kept for tests/tools).
func LoadFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
