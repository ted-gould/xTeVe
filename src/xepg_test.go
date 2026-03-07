package src

import "testing"

func TestEqualFoldNoSpaces(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		target   string
		expected bool
	}{
		{"identical", "abc", "abc", true},
		{"case differ", "AbC", "aBc", true},
		{"spaces in s", "a b c", "abc", true},
		{"spaces in target", "abc", "a b c", true},
		{"spaces in both", " a b c ", "a  b  c", true},
		{"different lengths without spaces", "abc", "ab", false},
		{"different characters", "abc", "abd", false},
		{"trailing spaces", "abc  ", "abc", true},
		{"trailing spaces diff", "abc  d", "abc", false},
		{"real world example", "Channel Name 4 1999 Variant 4", "ChannelName41999Variant4", true},
		{"real world diff case", "channel Name 4 1999 VARIANT 4", "ChannelName41999Variant4", true},
		{"unicode characters", "ÖSTERREICH", "österreich", true},
		{"unicode characters with spaces", "Ö S T E R R E I C H", "ö s t e r r e i c h", true},
		{"unicode characters diff", "ÖSTERREICH", "français", false},
		{"unicode characters accents", "FRANÇAIS", "français", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalFoldNoSpaces(tt.s, tt.target); got != tt.expected {
				t.Errorf("equalFoldNoSpaces(%q, %q) = %v, want %v", tt.s, tt.target, got, tt.expected)
			}
		})
	}
}