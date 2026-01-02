package src

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xteve/src/filecache"
)

func TestWebDAVCache_Integration(t *testing.T) {
	// Setup a temporary directory for cache
	tmpDir, err := os.MkdirTemp("", "webdav_cache_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a mock file
	const fileSize = 2 * 1024 * 1024 // 2MB
	mockData := make([]byte, fileSize)
	_, err = rand.Read(mockData)
	require.NoError(t, err)

	var requestCount int32

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		http.ServeContent(w, r, "test.bin", time.Now(), io.NewSectionReader(bytes.NewReader(mockData), 0, int64(len(mockData))))
	}))
	defer server.Close()

	// Initialize CacheManager manually with the temp dir
	cm, err := filecache.NewCacheManager(tmpDir)
	require.NoError(t, err)

	// Override global fileCache safely
	_ = getFileCache()
	oldCache := fileCache
	fileCache = cm
	defer func() { fileCache = oldCache }()

	// Construct a webdavStream
	streamMap := map[string]string{
		"url": server.URL,
	}

	ctx := context.Background()

	stream := &webdavStream{
		stream:    streamMap,
		name:      "test.bin",
		ctx:       ctx,
		targetURL: server.URL,
		size:      fileSize,
	}

	// 1. First Read: Should hit server and populate cache
	initialReqCount := atomic.LoadInt32(&requestCount)

	buf := make([]byte, 1024)
	n, err := io.ReadFull(stream, buf) // Use ReadFull to handle short reads
	require.NoError(t, err)
	assert.Equal(t, 1024, n)
	assert.Equal(t, mockData[:1024], buf[:n], "Data mismatch on first read")

	stream.Close()

	// Wait for cache file to reach 1MB
	// Note: We use the hash of the URL to check file existence directly in test
	// Ideally we would use cm.GetPath(server.URL) but that's internal logic.
	// But we can check cm.Exists(server.URL).

	require.Eventually(t, func() bool {
		return cm.Exists(server.URL)
	}, 5*time.Second, 100*time.Millisecond, "Cache file should exist")

	// Verify server was hit
	assert.Greater(t, atomic.LoadInt32(&requestCount), initialReqCount)

	// 2. Second Read: Should hit cache
	requestCountBeforeSecond := atomic.LoadInt32(&requestCount)

	stream2 := &webdavStream{
		stream:    streamMap,
		name:      "test.bin",
		ctx:       ctx,
		targetURL: server.URL,
		size:      fileSize,
	}

	n, err = io.ReadFull(stream2, buf)
	require.NoError(t, err)
	assert.Equal(t, 1024, n)
	assert.Equal(t, mockData[:1024], buf[:n], "Data mismatch on second read (cache)")
	stream2.Close()

	assert.Equal(t, requestCountBeforeSecond, atomic.LoadInt32(&requestCount), "Should serve from cache without hitting server")

	// 3. Third Read: Offset 1.5MB (Beyond cache). Should hit server.
	requestCountBeforeThird := atomic.LoadInt32(&requestCount)

	stream3 := &webdavStream{
		stream:    streamMap,
		name:      "test.bin",
		ctx:       ctx,
		targetURL: server.URL,
		size:      fileSize,
		pos:       1572864, // 1.5MB
	}

	n, err = io.ReadFull(stream3, buf)
	require.NoError(t, err)
	assert.Equal(t, 1024, n)

	// The robust handling (skip on 200 OK) ensures we get the correct data
	// even if the client/middleware strips the Range header in this test env.
	offset := 1572864
	assert.Equal(t, mockData[offset:offset+1024], buf[:n], "Data mismatch on third read (offset)")

	stream3.Close()

	assert.Greater(t, atomic.LoadInt32(&requestCount), requestCountBeforeThird, "Should hit server for non-cached range")
}
