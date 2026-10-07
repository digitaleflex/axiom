package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
)

type frame struct {
	id    int64
	event string
	data  deployment.Event
	ping  bool
}

type env struct {
	t        *testing.T
	store    *deployment.MemoryStore
	svc      *deployment.Service
	srv      *httptest.Server
	shutdown context.CancelFunc
	depID    string
}

func setup(t *testing.T, opts ...func(*Handler)) *env {
	t.Helper()
	store := deployment.NewMemoryStore()
	store.AddPlan(deployment.MemoryPlan{ID: "plan_1", ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Steps: []string{"BUILD", "VERIFY"}})
	svc := deployment.NewService(store, nil)
	rec, _, err := svc.Create(context.Background(), deployment.CreateInput{ApplicationID: "app_1", PlanID: "plan_1"})
	if err != nil {
		t.Fatal(err)
	}
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	h := &Handler{Store: store, Bus: svc.Events(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Shutdown: shutdownCtx, Heartbeat: time.Hour, Resync: time.Hour}
	for _, o := range opts {
		o(h)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/deployments/{deploymentID}/events/stream", h)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { shutdown(); srv.Close() })
	return &env{t: t, store: store, svc: svc, srv: srv, shutdown: shutdown, depID: rec.ID}
}

// open connects and returns a channel of parsed frames, closed at EOF.
func (e *env) open(lastEventID string) (<-chan frame, *http.Response) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/deployments/"+e.depID+"/events/stream", nil)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	ch := make(chan frame, 100)
	if resp.StatusCode != http.StatusOK {
		close(ch)
		return ch, resp
	}
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		var f frame
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if f.event != "" || f.ping {
					ch <- f
				}
				f = frame{}
			case strings.HasPrefix(line, ": ping"):
				f.ping = true
			case strings.HasPrefix(line, "id: "):
				f.id, _ = strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
			case strings.HasPrefix(line, "event: "):
				f.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f.data)
			}
		}
	}()
	return ch, resp
}

func next(t *testing.T, ch <-chan frame) frame {
	t.Helper()
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatal("stream closed unexpectedly")
			}
			if f.ping {
				continue
			}
			return f
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}

func expectClosed(t *testing.T, ch <-chan frame) {
	t.Helper()
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return
			}
			if !f.ping {
				t.Fatalf("expected stream end, got %+v", f)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("stream did not close")
		}
	}
}

func (e *env) advance(states ...deployment.State) {
	e.t.Helper()
	for _, s := range states {
		if _, err := e.svc.Transition(context.Background(), e.depID, s); err != nil {
			e.t.Fatal(err)
		}
	}
}

func TestStreamDeliversOrderedEventsAndEndsOnLive(t *testing.T) {
	e := setup(t)
	ch, resp := e.open("")
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	if f := next(t, ch); f.id != 1 || f.event != deployment.EventCreated {
		t.Fatalf("first frame = %+v", f)
	}
	e.advance(deployment.StateAnalyzing, deployment.StatePlanning, deployment.StateBuilding, deployment.StateDeploying, deployment.StateVerifying)
	for want := int64(2); want <= 6; want++ {
		if f := next(t, ch); f.id != want || f.event != deployment.EventStatusChanged {
			t.Fatalf("frame %d = %+v", want, f)
		}
	}
	if _, err := e.svc.MarkLive(context.Background(), e.depID, "https://app.example.com"); err != nil {
		t.Fatal(err)
	}
	f := next(t, ch)
	if f.id != 7 || f.data.Data["status"] != "LIVE" || f.data.Data["url"] != "https://app.example.com" {
		t.Fatalf("terminal frame = %+v", f)
	}
	expectClosed(t, ch)
}

func TestResumeWithLastEventID(t *testing.T) {
	e := setup(t)
	e.advance(deployment.StateAnalyzing, deployment.StatePlanning)
	ch, _ := e.open("2")
	if f := next(t, ch); f.id != 3 {
		t.Fatalf("resume must start after 2, got %+v", f)
	}
	e.advance(deployment.StateBuilding)
	if f := next(t, ch); f.id != 4 {
		t.Fatalf("live after resume = %+v", f)
	}
}

func TestGapRepairFromStore(t *testing.T) {
	e := setup(t, func(h *Handler) { h.Resync = 30 * time.Millisecond })
	ch, _ := e.open("")
	next(t, ch)
	// Write directly to the store: no bus notification (other instance / dropped event).
	for _, s := range []deployment.State{deployment.StateAnalyzing, deployment.StatePlanning} {
		if _, _, err := e.store.UpdateStatus(context.Background(), e.depID, deployment.StatusChange{To: s}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if f := next(t, ch); f.id != 2 {
		t.Fatalf("resync must deliver seq 2, got %+v", f)
	}
	if f := next(t, ch); f.id != 3 {
		t.Fatalf("resync must deliver seq 3, got %+v", f)
	}
	// A later bus event with a gap triggers immediate repair, without duplicates.
	if _, _, err := e.store.UpdateStatus(context.Background(), e.depID, deployment.StatusChange{To: deployment.StateBuilding}, nil); err != nil {
		t.Fatal(err)
	}
	e.advance(deployment.StateDeploying)
	got := []int64{next(t, ch).id, next(t, ch).id}
	if got[0] != 4 || got[1] != 5 {
		t.Fatalf("gap repair delivered %v, want [4 5]", got)
	}
}

func TestTerminalDeploymentReplaysThenCloses(t *testing.T) {
	e := setup(t)
	if _, err := e.svc.Cancel(context.Background(), e.depID); err != nil {
		t.Fatal(err)
	}
	ch, _ := e.open("")
	next(t, ch)
	if f := next(t, ch); f.data.Data["status"] != "CANCELLED" {
		t.Fatalf("expected CANCELLED, got %+v", f)
	}
	expectClosed(t, ch)

	ch, _ = e.open("2") // client already has everything
	expectClosed(t, ch)
}

func TestInvalidLastEventID(t *testing.T) {
	e := setup(t)
	_, resp := e.open("abc")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUnknownDeployment(t *testing.T) {
	e := setup(t)
	e.depID = "dep_missing"
	_, resp := e.open("")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHeartbeatAndShutdown(t *testing.T) {
	e := setup(t, func(h *Handler) { h.Heartbeat = 20 * time.Millisecond })
	ch, _ := e.open("")
	next(t, ch)
	select {
	case f := <-ch:
		if !f.ping {
			t.Fatalf("expected heartbeat, got %+v", f)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no heartbeat")
	}
	e.shutdown()
	expectClosed(t, ch)
}
