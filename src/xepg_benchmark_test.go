package src

import (
	"maps"
	"strconv"
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

func BenchmarkCleanupXEPGLogic(b *testing.B) {
	// Setup generic data
	numChannels := 10000
	activeRatio := 0.5

	// Simulate source IDs
	sourceIDs := []string{"source1"}

	// Prepare initial data maps to copy from in the loop
	initialChannels := make(map[string]XEPGChannelStruct, numChannels)
	activeStreams := make([]string, 0, int(float64(numChannels)*activeRatio))

	for i := 0; i < numChannels; i++ {
		id := strconv.Itoa(i)
		channel := XEPGChannelStruct{
			Name:      "Channel" + id,
			FileM3UID: "source1",
			XActive:   true,
		}

		// Mark some as "active" in the stream cache
		if float64(i) < float64(numChannels)*activeRatio {
			key := channel.Name + channel.FileM3UID
			activeStreams = append(activeStreams, key)
		}

		initialChannels[id] = channel
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Restore Data.XEPG.Channels for next run
		channels := maps.Clone(initialChannels)
		b.StartTimer()

		cleanupXEPGLogic(channels, activeStreams, sourceIDs)
	}
}
