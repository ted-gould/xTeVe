package m3u

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzMakeInterfaceFromM3U(f *testing.F) {
	// Helper function to add a file to the fuzzing corpus
	addFileToCorpus := func(path string) {
		content, err := os.ReadFile(path)
		if err == nil {
			f.Add(content)
		}
	}

	// Add seed corpus from various m3u files
	addFileToCorpus("test_playlist_1.m3u")

	// Add files from the testdata directory
	testdataDir := "../../testdata"
	addFileToCorpus(filepath.Join(testdataDir, "c-span.us.m3u"))

	benchmarkDir := filepath.Join(testdataDir, "benchmark_m3u")
	addFileToCorpus(filepath.Join(benchmarkDir, "example_fully_populated.m3u"))
	addFileToCorpus(filepath.Join(benchmarkDir, "large.m3u"))
	addFileToCorpus(filepath.Join(benchmarkDir, "medium.m3u"))
	addFileToCorpus(filepath.Join(benchmarkDir, "small.m3u"))
	addFileToCorpus(filepath.Join(benchmarkDir, "tiny.m3u"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// We are not checking the error here because we are only interested in panics.
		// The function is expected to return errors for malformed input.
		_, _ = MakeInterfaceFromM3U(data)
	})
}
