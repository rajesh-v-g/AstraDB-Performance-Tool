// Package sse provides a fan-out Server-Sent Events broadcaster.
package sse

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Broadcaster fans out events to all subscribed SSE clients.
// It is safe for concurrent use.
type Broadcaster struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

// NewBroadcaster creates a ready-to-use Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[chan string]struct{}),
	}
}

// Broadcast sends eventJSON to all currently subscribed clients.
// Sends are non-blocking: if a client's channel is full, the event is dropped
// for that client. Returns the number of clients that received the event.
func (b *Broadcaster) Broadcast(eventJSON string) int {
	msg := "data: " + eventJSON + "\n\n"
	b.mu.Lock()
	defer b.mu.Unlock()
	count := 0
	for ch := range b.clients {
		select {
		case ch <- msg:
			count++
		default:
			// Client is slow or disconnected; drop this event.
		}
	}
	return count
}

// Subscribe registers the caller as an SSE client and blocks until the client
// disconnects or the request context is cancelled.
//
// replayLog is the full log text to replay to the new client before live events
// begin, so late joiners see prior output immediately. Each line is sent as a
// separate SSE data frame.
func (b *Broadcaster) Subscribe(w http.ResponseWriter, r *http.Request, replayLog string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	// Replay prior log lines to the new client.
	if replayLog != "" {
		for _, line := range strings.Split(replayLog, "\n") {
			if line == "" {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		flusher.Flush()
	}

	// Register client channel (buffered 64 to absorb bursts).
	ch := make(chan string, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}()

	for {
		select {
		case msg, open := <-ch:
			if !open {
				return
			}
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// Close sends a terminal "done" event to all clients, closes all channels,
// and resets the client map.
func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	done := "data: {\"type\":\"done\"}\n\n"
	for ch := range b.clients {
		select {
		case ch <- done:
		default:
		}
		close(ch)
	}
	b.clients = make(map[chan string]struct{})
}
