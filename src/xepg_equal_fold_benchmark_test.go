package src

import "testing"

func BenchmarkEqualFoldNoSpaces(b *testing.B) {
	s1 := "Channel Name 10"
	s2 := "channelname10"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		equalFoldNoSpaces(s1, s2)
	}
}

func BenchmarkEqualFoldNoSpaces_MismatchEnd(b *testing.B) {
	s1 := "Channel Name 10"
	s2 := "channelname11"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		equalFoldNoSpaces(s1, s2)
	}
}

func BenchmarkEqualFoldNoSpaces_MismatchStart(b *testing.B) {
	s1 := "Channel Name 10"
	s2 := "xhannelname10"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		equalFoldNoSpaces(s1, s2)
	}
}
