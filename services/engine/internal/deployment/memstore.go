package deployment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryPlan is the subset of a plan the memory store needs.
type MemoryPlan struct {
	ID, ApplicationID, ServerID, Environment string
	Steps                                    []string
}

// MemoryStore is an in-process Store with the same semantics as the
// PostgreSQL store. It is intended for tests and database-less development.
type MemoryStore struct {
	mu      sync.Mutex
	plans   map[string]MemoryPlan
	records map[string]Record
	steps   map[string][]Step
	events  map[string][]Event
	idem    map[string]idemEntry
	numbers map[string]int
	planUse map[string]string
	now     func() time.Time
}

type idemEntry struct{ hash, id string }

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		plans: map[string]MemoryPlan{}, records: map[string]Record{}, steps: map[string][]Step{},
		events: map[string][]Event{}, idem: map[string]idemEntry{}, numbers: map[string]int{},
		planUse: map[string]string{}, now: func() time.Time { return time.Now().UTC() },
	}
}

// AddPlan registers a plan that deployments can be created from.
func (m *MemoryStore) AddPlan(p MemoryPlan) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[p.ID] = p
}

func (m *MemoryStore) Create(_ context.Context, in CreateInput) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idemKey := in.IdempotencyScope() + "\x00" + in.IdempotencyKey
	if in.IdempotencyKey != "" {
		if e, ok := m.idem[idemKey]; ok {
			if e.hash != in.RequestHash() {
				return Record{}, false, ErrIdempotencyConflict
			}
			return m.records[e.id], false, nil
		}
	}
	plan, ok := m.plans[in.PlanID]
	if !ok || plan.ApplicationID != in.ApplicationID {
		return Record{}, false, ErrPlanNotFound
	}
	if _, used := m.planUse[in.PlanID]; used {
		return Record{}, false, ErrPlanAlreadyUsed
	}
	now := m.now()
	m.numbers[in.ApplicationID]++
	rec := Record{
		ID: NewID("dep"), Number: m.numbers[in.ApplicationID], ApplicationID: in.ApplicationID,
		ServerID: plan.ServerID, Environment: plan.Environment, PlanID: plan.ID, Status: StatePending,
		CreatedBy: in.CreatedBy, CreatedAt: now, UpdatedAt: now,
	}
	m.records[rec.ID] = rec
	m.planUse[plan.ID] = rec.ID
	for i, name := range plan.Steps {
		m.steps[rec.ID] = append(m.steps[rec.ID], Step{Name: name, Position: i + 1, Status: StepQueued})
	}
	m.appendLocked(rec.ID, EventCreated, map[string]any{"status": StatePending, "planId": plan.ID, "number": rec.Number})
	if in.IdempotencyKey != "" {
		m.idem[idemKey] = idemEntry{hash: in.RequestHash(), id: rec.ID}
	}
	return rec, true, nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}

func (m *MemoryStore) List(_ context.Context, f ListFilter) ([]Record, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Record
	for _, r := range m.records {
		if r.ApplicationID == f.ApplicationID && (f.Environment == "" || r.Environment == f.Environment) && (f.Status == "" || string(r.Status) == f.Status) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number > out[j].Number })
	total := len(out)
	if f.Offset > len(out) {
		f.Offset = len(out)
	}
	out = out[f.Offset:]
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, total, nil
}

func (m *MemoryStore) Steps(_ context.Context, id string) ([]Step, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[id]; !ok {
		return nil, ErrNotFound
	}
	return append([]Step(nil), m.steps[id]...), nil
}

func (m *MemoryStore) UpdateStatus(_ context.Context, id string, change StatusChange, validate func(Record) error) (Record, Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return Record{}, Event{}, ErrNotFound
	}
	if validate != nil {
		if err := validate(rec); err != nil {
			return rec, Event{}, err
		}
	}
	from := rec.Status
	now := m.now()
	applyStatus(&rec, change, now)
	m.records[id] = rec
	ev := m.appendLocked(id, EventStatusChanged, statusEventData(from, rec))
	return rec, ev, nil
}

func (m *MemoryStore) UpdateStep(_ context.Context, id string, change StepChange) (Step, Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[id]; !ok {
		return Step{}, Event{}, ErrNotFound
	}
	for i, st := range m.steps[id] {
		if st.Name == change.Name {
			applyStep(&st, change, m.now())
			m.steps[id][i] = st
			ev := m.appendLocked(id, stepEventType(change.Status), stepEventData(st))
			return st, ev, nil
		}
	}
	return Step{}, Event{}, fmt.Errorf("%w: %s", ErrStepNotFound, change.Name)
}

func (m *MemoryStore) Events(_ context.Context, id string, afterSeq int64, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Event
	for _, e := range m.events[id] {
		if e.Seq > afterSeq {
			out = append(out, e)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (m *MemoryStore) appendLocked(id, typ string, data map[string]any) Event {
	ev := Event{ID: NewID("evt"), Seq: int64(len(m.events[id]) + 1), Type: typ, Version: 1, DeploymentID: id, OccurredAt: m.now(), Data: data}
	m.events[id] = append(m.events[id], ev)
	return ev
}

// --- helpers shared with the PostgreSQL store ---------------------------------

// NewID returns an opaque identifier with the given prefix (API contract §3).
func NewID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func applyStatus(rec *Record, change StatusChange, now time.Time) {
	rec.Status = change.To
	rec.UpdatedAt = now
	if change.URL != "" {
		rec.URL = change.URL
	}
	if change.ErrorCode != "" {
		rec.ErrorCode = change.ErrorCode
	}
	if rec.StartedAt == nil && change.To != StatePending {
		rec.StartedAt = &now
	}
	if change.To.Terminal() {
		rec.CompletedAt = &now
	}
}

func applyStep(st *Step, change StepChange, now time.Time) {
	st.Status = change.Status
	switch change.Status {
	case StepRunning:
		st.StartedAt = &now
	case StepCompleted, StepFailed, StepSkipped, StepCancelled:
		if st.StartedAt == nil && change.Status != StepSkipped {
			st.StartedAt = &now
		}
		st.CompletedAt = &now
	}
	if change.ExitCode != nil {
		st.ExitCode = change.ExitCode
	}
	if change.ErrorCode != "" {
		st.ErrorCode = change.ErrorCode
	}
}

// StatusEventData builds the data payload of a status event.
func statusEventData(from State, rec Record) map[string]any {
	d := map[string]any{"from": from, "status": rec.Status}
	if rec.URL != "" && rec.Status == StateLive {
		d["url"] = rec.URL
	}
	if rec.ErrorCode != "" && rec.Status == StateFailed {
		d["errorCode"] = rec.ErrorCode
	}
	return d
}

func stepEventType(s StepStatus) string {
	switch s {
	case StepRunning:
		return EventStepStarted
	case StepCompleted:
		return EventStepCompleted
	case StepFailed:
		return EventStepFailed
	default:
		return EventStepSkipped
	}
}

func stepEventData(st Step) map[string]any {
	d := map[string]any{"step": st.Name, "status": st.Status}
	if st.ExitCode != nil {
		d["exitCode"] = *st.ExitCode
	}
	if st.ErrorCode != "" {
		d["errorCode"] = st.ErrorCode
	}
	return d
}

// Exported aliases used by other Store implementations.
var (
	ApplyStatus     = applyStatus
	ApplyStep       = applyStep
	StatusEventData = statusEventData
	StepEventType   = stepEventType
	StepEventData   = stepEventData
)
