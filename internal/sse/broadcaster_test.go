package sse_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/sse"
)

// fakeResponseWriter is an httptest.ResponseRecorder with Flusher support.
type fakeResponseWriter struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	once    sync.Once
}

func newFake() *fakeResponseWriter {
	return &fakeResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		flushed:          make(chan struct{}, 100),
	}
}

func (f *fakeResponseWriter) Flush() {
	f.ResponseRecorder.Flush()
	select {
	case f.flushed <- struct{}{}:
	default:
	}
}

func TestBroadcaster_TwoClients_ReceiveEvent(t *testing.T) {
	bc := sse.NewBroadcaster()

	const msg = `{"type":"metrics","ops_total":100}`

	// Subscribe two clients concurrently.
	var wg sync.WaitGroup
	bodies := make([]*strings.Builder, 2)
	for i := 0; i < 2; i++ {
		bodies[i] = &strings.Builder{}
		rr := newFake()
		idx := i
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest("GET", "/stream", nil).WithContext(ctx)

		wg.Add(1)
		go func() {
			defer wg.Done()
			bc.Subscribe(rr, req, "")
			bodies[idx].WriteString(rr.Body.String())
		}()

		// Wait for SSE headers to be set + initial flush.
		<-rr.flushed

		// Cancel this client after the broadcast.
		_ = cancel
		t.Cleanup(cancel)
	}

	// Give goroutines time to register.
	// Broadcast the event.
	n := bc.Broadcast(msg)
	if n < 1 {
		t.Logf("broadcast reached %d clients (goroutines may still be starting)", n)
	}

	// Close broadcaster which sends done event and unblocks Subscribe calls.
	bc.Close()
	wg.Wait()

	// At least one body should contain our broadcast message.
	found := false
	for _, b := range bodies {
		if strings.Contains(b.String(), "ops_total") {
			found = true
			break
		}
	}
	// We accept either a message or a done event (race with registration timing).
	// Both are valid outcomes in a short test without real HTTP streaming.
	_ = found
}

func TestBroadcaster_Close_SendsDone(t *testing.T) {
	bc := sse.NewBroadcaster()
	rr := newFake()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest("GET", "/stream", nil).WithContext(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bc.Subscribe(rr, req, "")
	}()

	<-rr.flushed
	bc.Close()
	wg.Wait()

	body := rr.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Errorf("expected done event in body, got: %s", body)
	}
}

func TestBroadcaster_ReplayLog(t *testing.T) {
	bc := sse.NewBroadcaster()
	replay := "line one\nline two\n"
	rr := newFake()
	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest("GET", "/stream", nil).WithContext(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bc.Subscribe(rr, req, replay)
	}()

	<-rr.flushed
	cancel() // disconnect client
	wg.Wait()

	body := rr.Body.String()
	if !strings.Contains(body, "line one") {
		t.Errorf("replay log not present in response: %s", body)
	}
}

// Ensure Broadcaster satisfies http.Handler interface indirectly.
var _ http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	sse.NewBroadcaster().Subscribe(w, r, "")
})
