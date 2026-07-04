package src

import "testing"

func TestEqualFoldNoSpaces(t *testing.T) {
	tests := []struct {
		s1, s2 string
		want   bool
	}{
		{"Channel Name 10", "channelname10", true},
		{" Channel Name 10 ", "channelname10", true},
		{"channelname10", " Channel Name 10 ", true},
		{"Channel Name 10", "channelname11", false},
		{"Channel Name 10", "xhannelname10", false},
		{"A B C", "abc", true},
		{"  A  B  C  ", "abc", true},
		{"abc", "def", false},
		{"", "", true},
		{" ", "", true},
		{"", " ", true},
		{"   ", "   ", true},
		{"a", "A", true},
		{"a", "a", true},
		{"a", "b", false},
		{"a", " a", true},
		{"a", "a ", true},
		// Unicode
		{"Héllo", "héllo", true},
		{"Héllo World", "hélloworld", true},
		{"Héllo World", "hélloworldd", false},
        {"ß", "SS", false},
        {"ss", "ß", false},
        {"Δ", "δ", true}, // greek delta
        {"Δ", "Δ", true},
        {"Δ Δ", "δδ", true},
	}

    for _, tt := range tests {
		got := equalFoldNoSpaces(tt.s1, tt.s2)
		if got != tt.want {
			t.Errorf("equalFoldNoSpaces(%q, %q) = %v, want %v", tt.s1, tt.s2, got, tt.want)
		}
	}
}
