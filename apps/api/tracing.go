package main

import (
	"context"
	"crypto/tls"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
	"net/http"
	"os"
	"time"
)

func setupTracing(ctx context.Context) (*sdktrace.TracerProvider, error) {
	var exporter sdktrace.SpanExporter
	var err error
	sample := 0.1
	switch env("OTEL_MODE", "off") {
	case "off":
		return sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample())), nil
	case "console":
		exporter, err = stdouttrace.New(stdouttrace.WithWriter(os.Stdout))
		sample = 1
	case "gcp":
		project := os.Getenv("GCP_PROJECT")
		if project == "" {
			return nil, fail("INVALID", "GCP_PROJECT required for trace export")
		}
		auth, e := oauth.NewApplicationDefault(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if e != nil {
			return nil, fail("PERMANENT", "Trace export ADC unavailable")
		}
		exporter, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint("telemetry.googleapis.com:443"), otlptracegrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})), otlptracegrpc.WithDialOption(grpc.WithPerRPCCredentials(auth)), otlptracegrpc.WithHeaders(map[string]string{"x-goog-user-project": project}), otlptracegrpc.WithTimeout(5*time.Second), otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}))
	default:
		return nil, fail("INVALID", "Unknown trace operating mode")
	}
	if err != nil {
		return nil, fail("PERMANENT", "Trace exporter unavailable")
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "presspilot-api"), attribute.String("service.version", "0.1.0"), attribute.String("gcp.project_id", os.Getenv("GCP_PROJECT")))), sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sample)), sdktrace.WithBatcher(exporter, sdktrace.WithMaxQueueSize(256), sdktrace.WithMaxExportBatchSize(32), sdktrace.WithBatchTimeout(time.Second), sdktrace.WithExportTimeout(5*time.Second)))
	otel.SetTracerProvider(provider)
	return provider, nil
}

type traceWriter struct {
	http.ResponseWriter
	status int
}

func (w *traceWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *traceWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func tracedHTTP(provider trace.TracerProvider, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := "unmatched"
		switch r.URL.Path {
		case "/graphql", "/healthz", "/internal/tools", "/api/session", "/api/sandbox", "/api/sandbox/role", "/webhooks/shopify":
			route = r.URL.Path
		}
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := provider.Tracer("saero.presspilot").Start(ctx, "HTTP "+r.Method+" "+route, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", r.Method), attribute.String("http.route", route)))
		defer span.End()
		out := &traceWriter{ResponseWriter: w}
		next.ServeHTTP(out, r.WithContext(ctx))
		status := out.status
		if status == 0 {
			status = 200
		}
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, "Backend unavailable")
		}
	})
}
