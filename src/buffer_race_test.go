package src

import (
	"testing"
	"time"
)

// TestUpdateSegmentSentCount_ShiftedIndex verifies that updateSegmentSentCount
// correctly finds and increments SentCount for a segment by filename, even if
// the segment's index in the slice has shifted due to concurrent cleanup.
func TestUpdateSegmentSentCount_ShiftedIndex(t *testing.T) {
	playlistID := "test_playlist_shifted"
	streamID := 1

	// Setup initial state
	playlist := &Playlist{
		Streams: make(map[int]ThisStream),
	}

	// Create a stream with completed segments where the target file is NOT at its original index.
	// E.g., originally [0]="1.ts", [1]="2.ts", [2]="3.ts".
	// After cleanup of 1.ts, it's [0]="2.ts", [1]="3.ts".
	stream := ThisStream{
		CompletedSegments: []SegmentInfo{
			{Filename: "2.ts", SentCount: 0},
			{Filename: "3.ts", SentCount: 0},
		},
	}
	playlist.Streams[streamID] = stream
	BufferInformation.Store(playlistID, playlist)

	// Clean up after test
	defer BufferInformation.Delete(playlistID)

	// In the old code, if we sent "3.ts" which was originally at index 2,
	// we would call updateSegmentSentCount with segmentIndex=2 and filename="3.ts".
	// Since the slice only has length 2 now, index 2 would be out of bounds,
	// and the SentCount would not be updated.
	// With the fix, the index parameter is ignored and it finds "3.ts" by name.
	updateSegmentSentCount(playlistID, streamID, "3.ts")

	// Verify the update
	p, ok := BufferInformation.Load(playlistID)
	if !ok {
		t.Fatalf("Playlist not found in BufferInformation")
	}
	pl := p.(*Playlist)
	updatedStream, ok := pl.Streams[streamID]
	if !ok {
		t.Fatalf("Stream not found in playlist")
	}

	// 2.ts should still be 0
	if updatedStream.CompletedSegments[0].SentCount != 0 {
		t.Errorf("Expected 2.ts SentCount to be 0, got %d", updatedStream.CompletedSegments[0].SentCount)
	}

	// 3.ts should be 1
	if updatedStream.CompletedSegments[1].SentCount != 1 {
		t.Errorf("Expected 3.ts SentCount to be 1, got %d", updatedStream.CompletedSegments[1].SentCount)
	}
}

// TestCompleteTSsegment_Race verifies that completeTSsegment appends to the
// latest slice in BufferInformation rather than overwriting it with a stale local copy.
func TestCompleteTSsegment_Race(t *testing.T) {
	playlistID := "test_playlist_race"
	streamID := 1

	// Setup initial state
	playlist := &Playlist{
		Streams: make(map[int]ThisStream),
	}
	stream := ThisStream{
		CompletedSegments: []SegmentInfo{
			{Filename: "1.ts", SentCount: 1}, // Simulate client already processing and incrementing
		},
	}
	playlist.Streams[streamID] = stream
	BufferInformation.Store(playlistID, playlist)

	// Clean up after test
	defer BufferInformation.Delete(playlistID)

	// Simulate the local 'stream' copy held by the server loop which is STALE
	// (it thinks SentCount for 1.ts is 0, because it was copied before the client updated it).
	staleLocalStream := ThisStream{
		CompletedSegments: []SegmentInfo{
			{Filename: "1.ts", SentCount: 0},
		},
	}

	// Call completeTSsegment to add "2.ts".
	// In the old code, this would append to staleLocalStream's CompletedSegments,
	// and overwrite the map, resetting 1.ts SentCount back to 0.
	// In the fixed code, it loads the fresh stream from the map, appends to it,
	// and updates both the map and the local pointer.
	bandwidth := &BandwidthCalculation{
		Start: time.Now().Add(-1 * time.Second),
	}
	completeTSsegment(playlistID, streamID, &staleLocalStream, bandwidth, 1024, "tmp_2.ts", 2)

	// Verify the map
	p, ok := BufferInformation.Load(playlistID)
	if !ok {
		t.Fatalf("Playlist not found in BufferInformation")
	}
	pl := p.(*Playlist)
	updatedStream, ok := pl.Streams[streamID]
	if !ok {
		t.Fatalf("Stream not found in playlist")
	}

	if len(updatedStream.CompletedSegments) != 2 {
		t.Fatalf("Expected 2 completed segments, got %d", len(updatedStream.CompletedSegments))
	}

	// 1.ts should still have SentCount=1 (from the map), NOT 0 (from the stale local copy)
	if updatedStream.CompletedSegments[0].Filename != "1.ts" || updatedStream.CompletedSegments[0].SentCount != 1 {
		t.Errorf("Expected 1.ts SentCount to be 1, got %d", updatedStream.CompletedSegments[0].SentCount)
	}

	// 2.ts should be appended
	if updatedStream.CompletedSegments[1].Filename != "2.ts" || updatedStream.CompletedSegments[1].SentCount != 0 {
		t.Errorf("Expected 2.ts to be appended with SentCount 0")
	}

	// Verify that the local stale pointer was updated
	if len(staleLocalStream.CompletedSegments) != 2 || staleLocalStream.CompletedSegments[0].SentCount != 1 {
		t.Errorf("Expected local stream to be updated with the fresh map data")
	}
}
