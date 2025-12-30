package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

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
