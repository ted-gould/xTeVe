package src

import (
	"hash/maphash"
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

func BenchmarkGenerateChannelHash(b *testing.B) {
	m3uID := "test_m3u_id"
	name := "Test Channel"
	groupTitle := "Test Group"
	tvgID := "test.tvg.id"
	tvgName := "Test Tvg Name"
	uuidKey := "uuid_key"
	uuidValue := "uuid_value"

	var h maphash.Hash

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		generateChannelHash(&h, m3uID, name, groupTitle, tvgID, tvgName, uuidKey, uuidValue)
	}
}
