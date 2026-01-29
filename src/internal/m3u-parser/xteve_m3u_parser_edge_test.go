package m3u

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMakeInterfaceFromM3U_EdgeCases_Optimization(t *testing.T) {
	input := `#EXTM3U
#EXTINF:0,Standard Stream
http://example.com/standard
#EXTINF:0,Relative Stream
stream/1.ts
#EXTINF -1,Malformed Header
http://example.com/malformed
#EXTINF:0,Magnet Link
magnet:?xt=urn:btih:12345
#EXTINF:0,UDP Stream
udp://@239.0.0.1:1234
`

	rawStreams, err := MakeInterfaceFromM3U([]byte(input))
	assert.NoError(t, err)
	assert.Len(t, rawStreams, 5)

	// Helper to get stream by index
	getStream := func(i int) map[string]string {
		return rawStreams[i].(map[string]string)
	}

	// Stream 1: Standard
	s1 := getStream(0)
	assert.Equal(t, "Standard Stream", s1["name"])
	assert.Equal(t, "http://example.com/standard", s1["url"])

	// Stream 2: Relative (Currently might fail/be broken in old code, checking desired behavior)
	s2 := getStream(1)
	assert.Equal(t, "Relative Stream", s2["name"])
	assert.Equal(t, "stream/1.ts", s2["url"])

	// Stream 3: Malformed Header
	s3 := getStream(2)
	assert.Contains(t, s3["name"], "Malformed Header") // Name parsing might vary depending on how strict it is, but it shouldn't crash
	assert.Equal(t, "http://example.com/malformed", s3["url"])

	// Stream 4: Magnet Link (Edge case for URL detection)
	s4 := getStream(3)
	assert.Equal(t, "Magnet Link", s4["name"])
	assert.Equal(t, "magnet:?xt=urn:btih:12345", s4["url"])

	// Stream 5: UDP Stream
	s5 := getStream(4)
	assert.Equal(t, "UDP Stream", s5["name"])
	assert.Equal(t, "udp://@239.0.0.1:1234", s5["url"])
}
