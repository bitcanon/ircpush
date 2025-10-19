package irc

import (
	"strings"
	"testing"
)

// TestEnsureChanPrefix tests the ensureChanPrefix function to verify that it
// correctly adds the '#' prefix to channel names that do not already have it.
func TestEnsureChanPrefix(t *testing.T) {
	// Setup test cases
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ChannelWithHashPrefix",
			input:    "#channel1",
			expected: "#channel1",
		},
		{
			name:     "ChannelWithAmpersandPrefix",
			input:    "&channel2",
			expected: "&channel2",
		},
		{
			name:     "ChannelWithoutPrefix",
			input:    "channel3",
			expected: "#channel3",
		},
		{
			name:     "ChannelWithLeadingAndTrailingSpaces",
			input:    "  channel4  ",
			expected: "#channel4",
		},
		{
			name:     "EmptyChannelName",
			input:    "",
			expected: "",
		},
	}
	// Run test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Call the function with the test input
			output := ensureChanPrefix(test.input)

			// Compare the output to the expected value
			if output != test.expected {
				t.Errorf("expected %q, but got %q", test.expected, output)
			}
		})
	}
}

// TestServerName tests the serverName function to verify that it correctly
// extracts the hostname from an address string.
func TestServerName(t *testing.T) {
	// Setup test cases
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "HostWithPort",
			input:    "irc.example.com:6667",
			expected: "irc.example.com",
		},
		{
			name:     "HostWithoutPort",
			input:    "irc.example.org",
			expected: "irc.example.org",
		},
		{
			name:     "IPv4WithPort",
			input:    "1.2.3.4:6697",
			expected: "1.2.3.4",
		},
		{
			name:     "IPv4WithoutPort",
			input:    "1.2.3.4",
			expected: "1.2.3.4",
		},
	}
	// Run test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Call the function with the test input
			output := serverName(test.input)

			// Compare the output to the expected value
			if output != test.expected {
				t.Errorf("expected %q, but got %q", test.expected, output)
			}
		})
	}
}

// TestLogf tests the logf function to verify that it correctly writes
// formatted logs to the provided writer.
func TestLogf(t *testing.T) {
	// Setup test cases
	tests := []struct {
		name     string
		inputFmt string
		inputArg []any
		expected string
	}{
		{
			name:     "SimpleMessage",
			inputFmt: "Hello, World!",
			inputArg: nil,
			expected: "Hello, World!",
		},
		{
			name:     "FormattedMessage",
			inputFmt: "User %s has joined the channel.",
			inputArg: []any{"alice"},
			expected: "User alice has joined the channel.",
		},
		{
			name:     "MultipleArguments",
			inputFmt: "%d users are online in %s.",
			inputArg: []any{5, "#general"},
			expected: "5 users are online in #general.",
		},
	}
	// Run test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf strings.Builder
			logf(&buf, test.inputFmt, test.inputArg...)

			output := buf.String()
			// logf adds a trailing newline; ignore it for comparison
			output = strings.TrimSuffix(output, "\n")

			if output != test.expected {
				t.Errorf("expected %q, but got %q", test.expected, output)
			}
		})
	}
}
