package http

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestExecutionTimerStopsAtHeaders(t *testing.T) {
	t.Parallel()

	client := &Client{config: ClientOptions{Timeout: 10 * time.Millisecond, HTTPClient: new(http.Client)}}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	exec, err := client.newExecution(request, &RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}

	defer exec.release()

	exec.headersReceived.Store(true)
	time.Sleep(20 * time.Millisecond)

	if err := exec.ctx.Err(); err != nil {
		t.Fatalf("execution context ended after headers: %v", err)
	}
}

func TestExecutionTimerExpiresBeforeHeaders(t *testing.T) {
	t.Parallel()

	client := &Client{config: ClientOptions{Timeout: 10 * time.Millisecond, HTTPClient: new(http.Client)}}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	exec, err := client.newExecution(request, &RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}

	defer exec.release()

	time.Sleep(20 * time.Millisecond)

	if cause := context.Cause(exec.ctx); !errors.Is(cause, context.DeadlineExceeded) {
		t.Fatalf("execution context cause = %v, want %v", cause, context.DeadlineExceeded)
	}
}
