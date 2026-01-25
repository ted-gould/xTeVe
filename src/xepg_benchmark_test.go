package src

import (
	"testing"
)

func BenchmarkAdjustProgramTime(b *testing.B) {
	t := "20241225120000 +0000"
	timeshift := 2
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adjustProgramTime(t, timeshift)
	}
}
