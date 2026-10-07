// Package sse serves the realtime deployment event stream (issue #118,
// ADR-0009): GET /api/v1/deployments/{deploymentID}/events/stream.
//
// Guarantees:
//   - every event carries `id: <seq>`; reconnecting clients send Last-Event-ID
//     (browsers do so automatically) and receive exactly the events after it;
//   - persisted events are the source of truth: the in-process bus is only a
//     low-latency notifier, and gaps (slow subscriber, other Engine instance)
//     are repaired from the store;
//   - events are delivered in seq order without duplicates;
//   - the stream ends after the terminal status event (LIVE/FAILED/CANCELLED),
//     when the client disconnects, or when the Engine shuts down.
package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
)

// Store is the read side the stream needs.
type Store interface {
	Get(ctx context.Context, id string) (deployment.Record, error)
	Events(ctx context.Context, id string, afterSeq int64, limit int) ([]deployment.Event, error)
}

// Handler streams deployment events.
type Handler struct {
	Store Store
	Bus   *deployment.EventBus
	Log   *slog.Logger
	// Shutdown ends all streams when cancelled (Engine graceful shutdown).
	Shutdown context.Context
	// Heartbeat interval for comment frames keeping proxies from timing out.
	Heartbeat time.Duration
	// Resync interval: polls the store to catch events published by other
	// Engine instances or dropped by the bus.
	Resync time.Duration
	// Retry is the reconnection delay advertised to clients.
	Retry time.Duration
}

const pageSize = 500

func (h *Handler) defaults() {
	if h.Log == nil {
		h.Log = slog.Default()
	}
	if h.Shutdown == nil {
		h.Shutdown = context.Background()
	}
	if h.Heartbeat <= 0 {
		h.Heartbeat = 15 * time.Second
	}
	if h.Resync <= 0 {
		h.Resync = 5 * time.Second
	}
	if h.Retry <= 0 {
		h.Retry = 3 * time.Second
	}
}

// ServeHTTP implements the stream. Access control is enforced by the caller
// (the API wraps this handler with its deployment ownership check).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.defaults()
	id := r.PathValue("deploymentID")
	lastSeq, err := lastEventID(r)
	if err != nil {
		http.Error(w, `{"error":{"code":"INVALID_REQUEST","message":"Last-Event-ID must be a non-negative integer","details":{}}}`, http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(h.Shutdown, cancel)
	defer stop()

	// Subscribe before reading the store so no event can fall between them.
	live, unsubscribe := h.Bus.Subscribe(id)
	defer unsubscribe()

	rec, err := h.Store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, deployment.ErrNotFound) {
			http.Error(w, `{"error":{"code":"NOT_FOUND","message":"deployment not found","details":{}}}`, http.StatusNotFound)
			return
		}
		h.Log.Error("sse: load deployment", "deploymentId", id, "error", err)
		http.Error(w, `{"error":{"code":"INTERNAL_ERROR","message":"stream unavailable","details":{}}}`, http.StatusInternalServerError)
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache, no-transform")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
	w.WriteHeader(http.StatusOK)

	s := &stream{w: w, f: flusher, lastSeq: lastSeq}
	s.raw(fmt.Sprintf("retry: %d\n\n", h.Retry.Milliseconds()))

	// Catch up from the store.
	done, err := s.catchUp(ctx, h.Store, id)
	if err != nil || done || s.err != nil {
		return
	}
	// Already terminal and nothing new after lastSeq: nothing more will come.
	if rec.Status.Terminal() && s.lastSeq > 0 {
		if latest, _ := h.Store.Events(ctx, id, s.lastSeq, 1); len(latest) == 0 {
			return
		}
	}

	heartbeat := time.NewTicker(h.Heartbeat)
	defer heartbeat.Stop()
	resync := time.NewTicker(h.Resync)
	defer resync.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-live:
			if !ok {
				return
			}
			switch {
			case ev.Seq <= s.lastSeq:
				continue // already delivered
			case ev.Seq == s.lastSeq+1:
				if s.send(ev) {
					return
				}
			default: // gap: repair from the store
				if done, err := s.catchUp(ctx, h.Store, id); err != nil || done {
					return
				}
			}
		case <-resync.C:
			if done, err := s.catchUp(ctx, h.Store, id); err != nil || done {
				return
			}
		case <-heartbeat.C:
			s.raw(": ping\n\n")
		}
		if s.err != nil {
			return
		}
	}
}

type stream struct {
	w       http.ResponseWriter
	f       http.Flusher
	lastSeq int64
	err     error
}

// catchUp sends every persisted event after lastSeq. done reports a terminal event.
func (s *stream) catchUp(ctx context.Context, store Store, id string) (bool, error) {
	for {
		events, err := store.Events(ctx, id, s.lastSeq, pageSize)
		if err != nil {
			return false, err
		}
		for _, ev := range events {
			if s.send(ev) {
				return true, nil
			}
			if s.err != nil {
				return false, s.err
			}
		}
		if len(events) < pageSize {
			return false, nil
		}
	}
}

// send writes one event and reports whether it was terminal.
func (s *stream) send(ev deployment.Event) bool {
	payload, err := json.Marshal(ev)
	if err != nil {
		s.err = err
		return false
	}
	s.raw("id: " + strconv.FormatInt(ev.Seq, 10) + "\nevent: " + ev.Type + "\ndata: " + string(payload) + "\n\n")
	s.lastSeq = ev.Seq
	return isTerminal(ev)
}

func (s *stream) raw(frame string) {
	if s.err != nil {
		return
	}
	if _, err := s.w.Write([]byte(frame)); err != nil {
		s.err = err
		return
	}
	s.f.Flush()
}

func isTerminal(ev deployment.Event) bool {
	if ev.Type != deployment.EventStatusChanged {
		return false
	}
	// Live events may carry deployment.State values; persisted ones carry strings.
	return deployment.State(fmt.Sprint(ev.Data["status"])).Terminal()
}

// lastEventID reads Last-Event-ID (header, set by browsers on reconnect) or
// the lastEventId query parameter (initial connections that need to resume).
func lastEventID(r *http.Request) (int64, error) {
	v := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if v == "" {
		v = r.URL.Query().Get("lastEventId")
	}
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("invalid Last-Event-ID")
	}
	return n, nil
}
