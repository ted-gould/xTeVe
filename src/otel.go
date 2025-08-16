package src

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
)

var tracer = otel.Tracer("xteve")

// TracerProvider is the tracer provider for the application.
var TracerProvider *sdktrace.TracerProvider

// InitOtel initializes the OpenTelemetry tracer provider.
func InitOtel() (err error) {
	// Create a new OTLP HTTP exporter.
	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint(Settings.OtelGRPCEndpoint),
	)
	if err != nil {
		return err
	}

	// Create a new tracer provider with the exporter.
	TracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(Settings.OtelServiceName),
		)),
	)

	// Set the global tracer provider.
	otel.SetTracerProvider(TracerProvider)

	// Set the propagator for context propagation.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return nil
}
