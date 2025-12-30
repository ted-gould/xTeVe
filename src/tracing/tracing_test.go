package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"net"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type mockTraceServer struct {
	coltracepb.UnimplementedTraceServiceServer
	headers metadata.MD
}

func (s *mockTraceServer) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		s.headers = md
	}
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

func TestExporterSelection(t *testing.T) {
	// Test case 1: OTEL_EXPORTER_TYPE is "otlp"
	t.Run("otlp", func(t *testing.T) {
		exporter, err := newSpanExporter(context.Background(), ExporterTypeOTLP)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
		assert.IsType(t, &otlptrace.Exporter{}, exporter)
	})

	// Test case 2: OTEL_EXPORTER_TYPE is not set (defaults to stdout)
	t.Run("stdout", func(t *testing.T) {
		exporter, err := newSpanExporter(context.Background(), ExporterTypeStdout)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
		assert.IsType(t, &stdouttrace.Exporter{}, exporter)
	})

	// Test case 3: OTEL_EXPORTER_OTLP_HEADERS is set
	t.Run("otlp_headers", func(t *testing.T) {
		os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x-axiom-dataset=test-dataset,Authorization=Bearer token")
		defer os.Unsetenv("OTEL_EXPORTER_OTLP_HEADERS")

		exporter, err := newSpanExporter(context.Background(), ExporterTypeOTLP)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
		assert.IsType(t, &otlptrace.Exporter{}, exporter)
	})
}

func TestOTLPHeadersParsing(t *testing.T) {
	// Start a mock gRPC server
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	mockServer := &mockTraceServer{}
	coltracepb.RegisterTraceServiceServer(s, mockServer)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("Server exited: %v", err)
		}
	}()
	defer s.Stop()

	// Configure environment variables
	// Note: Endpoint needs a scheme (http://) to be parsed correctly by the SDK,
	// even for gRPC, or it triggers "first path segment in URL cannot contain colon".
	os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+lis.Addr().String())
	os.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "key1=value1,key2=value2")
	defer func() {
		os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		os.Unsetenv("OTEL_EXPORTER_OTLP_INSECURE")
		os.Unsetenv("OTEL_EXPORTER_OTLP_HEADERS")
	}()

	// Create exporter
	ctx := context.Background()
	exporter, err := newSpanExporter(ctx, ExporterTypeOTLP)
	assert.NoError(t, err)

	// Create a TracerProvider with the exporter
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)

	// Create a span to trigger export
	tracer := tp.Tracer("test-tracer")
	_, span := tracer.Start(ctx, "test-span")
	span.End()

	// Force flush to ensure data is sent
	err = tp.ForceFlush(ctx)
	assert.NoError(t, err)

	// Verify headers were received
	// gRPC metadata keys are lowercased
	if assert.Contains(t, mockServer.headers, "key1") {
		assert.Equal(t, "value1", mockServer.headers["key1"][0])
	}
	if assert.Contains(t, mockServer.headers, "key2") {
		assert.Equal(t, "value2", mockServer.headers["key2"][0])
	}

	// Clean up
	err = tp.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestTracingMiddleware(t *testing.T) {
	// Create an in-memory exporter
	exporter := tracetest.NewInMemoryExporter()

	// Create a new tracer provider with the in-memory exporter
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	// Create a new test server with the handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(otelhttp.NewHandler(handler, "test"))
	defer server.Close()

	// Make a request to the test server
	req, _ := http.NewRequest("GET", server.URL, nil)
	client := &http.Client{}
	_, err := client.Do(req)
	assert.NoError(t, err)

	// Force flush the exporter
	err = tp.ForceFlush(context.Background())
	assert.NoError(t, err)

	// Check that a span was created
	spans := exporter.GetSpans()
	assert.Len(t, spans, 1)

	// Shut down the tracer provider
	err = tp.Shutdown(context.Background())
	assert.NoError(t, err)
}
