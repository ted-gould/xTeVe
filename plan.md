1. **Understand Context in `bufferingStream`**:
   - In `src/buffer.go`, `bufferingStream` extracts a span from the request context and creates a detached context for the goroutine `connectToStreamingServer`.
   - The detached context gets passed correctly, but we need to ensure traces are properly created within `connectToStreamingServer`.

2. **Add Trace Spans to `connectToStreamingServer`**:
   - At the beginning of `connectToStreamingServer`, extract the context and start a new span, e.g., `tracer.Start(ctx, "connectToStreamingServer")`.
   - Add useful attributes to the span like `streamID`, `playlistID`, `channelName`, `streamingURL`, `playlistName`.

3. **Pass Context and Add Trace Spans to Helper Functions**:
   - `processSegments`: Extract context, start a span `processSegments`, add attributes.
   - `processStreamingServerResponse`: Already takes `ctx`, start a span `processStreamingServerResponse`, add attributes (`currentURL`, etc).
   - `handleHLSStream`: Already takes `ctx`, start a span `handleHLSStream`.
   - `handleTSStream`: Doesn't take `ctx`, modify it to take `ctx context.Context`. Add trace span `handleTSStream` and attributes.
   - Update `processStreamingServerResponse` to pass `ctx` to `handleTSStream`.

4. **Verify Implementation**:
   - Build using `make build`.
   - Run tests `make test` or `go test ./src/...`.

5. **Submit**:
   - Pre-commit steps.
   - Submit.
