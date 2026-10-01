package main

import (
	"context"
	"encoding/json"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTracingPropagatesWithoutCapturingSecrets(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	handler := tracedHTTP(provider, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	request := httptest.NewRequest("POST", "http://localhost/graphql?access_token=private-marker", strings.NewReader("private-marker"))
	request.Header.Set("Authorization", "Bearer private-marker")
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatal("Trace context was not preserved")
	}
	b, _ := json.Marshal(spans)
	if strings.Contains(string(b), "private-marker") {
		t.Fatal("Trace captured request secrets")
	}
	if spans[0].Name != "HTTP POST /graphql" {
		t.Fatal("Unbounded route recorded")
	}
}
