package config

import (
	"testing"
)

// TestLoadFile tests the LoadFile function to verify that it correctly
// loads and parses a YAML configuration file into a Config struct.
func TestLoadFile(t *testing.T) {
	// Setup test cases
	tests := []struct {
		name        string
		filePath    string
		expected    *Config
		expectedErr bool
	}{
		{
			name:     "ValidConfigFile",
			filePath: "testdata/valid_config.yaml",
			expected: &Config{
				TCP: TCPConfig{
					Listen:       ":6667",
					MaxLineBytes: 65536,
				},
				IRC: IRCConfig{
					Server:        "irc.example.com:6667",
					TLS:           true,
					TLSSkipVerify: false,
					TLSClientCert: "/path/to/cert.pem",
					TLSClientKey:  "/path/to/key.pem",
					Nick:          "testbot",
					Realname:      "Test Bot",
					ServerPass:    "serverpass",
					IdentifyPass:  "identifypass",
					SASLExternal:  false,
					SASLLogin:     "sasluser",
					SASLPass:      "saslpass",
					Channels:      []string{"#channel1", "&channel2"},
					Keys: map[string]string{
						"#channel1": "key1",
						"&channel2": "key2",
					},
				},
			},
			expectedErr: false,
		},
		{
			name:        "NonExistentFile",
			filePath:    "testdata/non_existent.yaml",
			expected:    nil,
			expectedErr: true,
		},
		{
			name:        "InvalidYAMLFile",
			filePath:    "testdata/invalid_config.yaml",
			expected:    nil,
			expectedErr: true,
		},
	}

	// Run test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Call the function with the test input
			cfg, err := LoadFile(test.filePath)

			// Check for expected error
			if test.expectedErr {
				if err == nil {
					t.Errorf("expected an error but got nil")
				}
				return
			} else {
				if err != nil {
					t.Errorf("did not expect an error but got: %v", err)
					return
				}
			}

			// Compare the output to the expected value
			if !compareConfigs(cfg, test.expected) {
				t.Errorf("expected %+v, but got %+v", test.expected, cfg)
			}
		})
	}
}

// compareConfigs compares two Config structs for equality.
func compareConfigs(a, b *Config) bool {
	if a.TCP.Listen != b.TCP.Listen || a.TCP.MaxLineBytes != b.TCP.MaxLineBytes {
		return false
	}
	if a.IRC.Server != b.IRC.Server ||
		a.IRC.TLS != b.IRC.TLS ||
		a.IRC.TLSSkipVerify != b.IRC.TLSSkipVerify ||
		a.IRC.TLSClientCert != b.IRC.TLSClientCert ||
		a.IRC.TLSClientKey != b.IRC.TLSClientKey ||
		a.IRC.Nick != b.IRC.Nick ||
		a.IRC.Realname != b.IRC.Realname ||
		a.IRC.ServerPass != b.IRC.ServerPass ||
		a.IRC.IdentifyPass != b.IRC.IdentifyPass ||
		a.IRC.SASLExternal != b.IRC.SASLExternal ||
		a.IRC.SASLLogin != b.IRC.SASLLogin ||
		a.IRC.SASLPass != b.IRC.SASLPass {
		return false
	}
	if len(a.IRC.Channels) != len(b.IRC.Channels) {
		return false
	}
	for i := range a.IRC.Channels {
		if a.IRC.Channels[i] != b.IRC.Channels[i] {
			return false
		}
	}
	if len(a.IRC.Keys) != len(b.IRC.Keys) {
		return false
	}
	for k, v := range a.IRC.Keys {
		if b.IRC.Keys[k] != v {
			return false
		}
	}
	return true
}
