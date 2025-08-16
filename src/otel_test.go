package src

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func TestOtelMiddleware(t *testing.T) {
	// Create a mock exporter.
	exporter := tracetest.NewInMemoryExporter()

	// Create a new tracer provider with the mock exporter.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("xteve-test"),
		)),
	)

	// Set the global tracer provider.
	otel.SetTracerProvider(tp)

	// Set the propagator for context propagation.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	// Create a test HTTP server with the otelhttp middleware.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(otelhttp.NewHandler(handler, "test"))
	defer server.Close()

	// Send a request to the test server.
	req, err := http.NewRequest("GET", server.URL, nil)
	assert.NoError(t, err)
	client := &http.Client{}
	_, err = client.Do(req)
	assert.NoError(t, err)

	// Check that a span was created and exported to the mock exporter.
	assert.NoError(t, tp.ForceFlush(context.Background()))
	spans := exporter.GetSpans()
	assert.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "test", span.Name)
	assert.Equal(t, "xteve-test", span.Resource.Attributes()[0].Value.AsString())
}
