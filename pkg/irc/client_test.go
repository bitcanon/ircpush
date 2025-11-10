package irc

import (
	"reflect"
	"strings"
	"testing"

	"github.com/bitcanon/ircpush/pkg/config"
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

func TestSegmentMessage_NoLimit(t *testing.T) {
	c := &Client{cfg: config.IRCConfig{MaxMessageLen: 0}}
	msg := "this message should remain intact even if long 😊🚀"
	out := c.segmentMessage(msg)
	if len(out) != 1 || out[0] != msg {
		t.Fatalf("expected original message unchanged, got %v", out)
	}
}

func TestSegmentMessage_TruncateWithEllipsis(t *testing.T) {
	c := &Client{cfg: config.IRCConfig{MaxMessageLen: 5, SplitLong: false}}
	msg := "abcdefghi"
	// limit=5 -> since >3 expect first (5-3)=2 chars + "..."
	expected := "ab..."
	out := c.segmentMessage(msg)
	if len(out) != 1 || out[0] != expected {
		t.Fatalf("expected %q, got %v", expected, out)
	}
}

func TestSegmentMessage_TruncateNoEllipsis(t *testing.T) {
	c := &Client{cfg: config.IRCConfig{MaxMessageLen: 3, SplitLong: false}}
	msg := "abcdef"
	expected := "abc"
	out := c.segmentMessage(msg)
	if len(out) != 1 || out[0] != expected {
		t.Fatalf("expected %q, got %v", expected, out)
	}
}

func TestSegmentMessage_SplitLong_BreakOnSpace(t *testing.T) {
	c := &Client{cfg: config.IRCConfig{MaxMessageLen: 30, SplitLong: true}}
	msg := "Hello this is a message that should be split properly. Let's see how it works! :)"
	// Based on algorithm, expected segments are: "hello", "world", "it's me"
	expected := []string{"Hello this is a message that", "should be split properly.", "Let's see how it works! :)"}
	out := c.segmentMessage(msg)
	if !reflect.DeepEqual(out, expected) {
		t.Fatalf("expected %#v, got %#v", expected, out)
	}
	// Also ensure none of the returned segments exceed the limit in runes
	for _, seg := range out {
		if len([]rune(seg)) > c.cfg.MaxMessageLen {
			t.Fatalf("segment %q exceeds max length %d", seg, c.cfg.MaxMessageLen)
		}
		// And segments should not start with a space
		if strings.HasPrefix(seg, " ") {
			t.Fatalf("segment %q starts with a space", seg)
		}
	}
}

func TestSegmentMessage_UTF8Handling(t *testing.T) {
	// Multi-byte runes (emojis) should be counted as single runes.
	c := &Client{cfg: config.IRCConfig{MaxMessageLen: 3, SplitLong: false}}
	msg := "😊😊😊😊" // 4 runes
	out := c.segmentMessage(msg)
	if len(out) != 1 {
		t.Fatalf("expected single segment, got %v", out)
	}
	// With limit 3 and no ellipsis (limit <= 3) we expect first 3 runes
	if len([]rune(out[0])) != 3 {
		t.Fatalf("expected 3 runes in output, got %d (output=%q)", len([]rune(out[0])), out[0])
	}
}
