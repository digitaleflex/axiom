// Package state implements the Agent's durable local operation state store
// (#81): enough persisted state to recover safely after an Agent restart.
//
// # What is stored
//
// Every dispatched operation is recorded as one Entry keyed by its operation
// ID (op_<deploymentID>_<STEP>_<attempt>, docs/architecture/agent-protocol.md
// §6). An Entry carries the deployment, type, attempt, lifecycle phase,
// timestamps and the terminal result/error. It is the agent-side answer to
// "what was I doing when I stopped?".
//
// # File format
//
// The store is an append-only JSON-lines log. Each mutation appends one
// self-describing record and fsyncs before returning, so a crash can only lose
// the record that was mid-write, never a previously flushed one:
//
//		{"v":1,"seq":7,"crc":"a1b2c3d4","entry":{ ...Entry... }}
//
//	  - v   is the format version (currently 1).
//	  - seq is a monotonically increasing record sequence (diagnostics).
//	  - crc is the CRC-32 (IEEE) of the raw "entry" bytes.
//	  - entry is the marshalled Entry.
//
// On load every record is CRC-checked. A truncated tail record (a process that
// died mid-append) is dropped; a corrupt record in the middle stops the replay
// at that point so no unreliable state is applied. In both cases the records
// already replayed are kept and a *CorruptionError wrapping ErrCorrupt is
// returned alongside the usable store — corruption is recoverable, never
// silently fatal and never destructive to previously flushed entries.
//
// # Atomic writes
//
// Compaction (and the repair of a corrupt file on load) rewrites the log
// atomically: a fresh snapshot is written to <path>.tmp, fsynced, renamed over
// <path>, and the containing directory is fsynced. The file is created mode
// 0600 (the parent directory mode 0700 when created). A leftover .tmp file is
// never read.
//
// # Restart classification
//
// Opening a store is, by definition, a restart of the process. Any entry still
// in RECEIVED or RUNNING was interrupted; Open transitions it to INTERRUPTED,
// sets the recovery marker (Interrupted=true) and persists the transition. A
// terminal entry (COMPLETED, FAILED, INTERRUPTED) is immutable and is never
// re-executed or overwritten: Begin returns it unchanged and Complete/Fail/
// Interrupt are no-ops on it. This is the durable guarantee behind "completed
// operations are not blindly re-executed".
//
// Resumability is a property of the operation type. Only VERIFY is idempotent
// by design (a read-only probe), so only VERIFY is marked Resumable=true;
// mutating operations (CREATE_RUNTIME, NETWORK, START, STOP, REMOVE) may have
// partial effects and require reconciliation with the runtime
// (services/agent/internal/recovery, #82). See ResumableOperation.
package state

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
)

// Phase is the lifecycle phase of an operation. The set is closed.
type Phase string

const (
	// PhaseReceived means the operation was accepted and persisted but not yet
	// started. It is active (non-terminal).
	PhaseReceived Phase = "RECEIVED"
	// PhaseRunning means execution has begun. It is active (non-terminal).
	PhaseRunning Phase = "RUNNING"
	// PhaseCompleted means execution finished successfully. Terminal.
	PhaseCompleted Phase = "COMPLETED"
	// PhaseFailed means execution finished with an error. Terminal.
	PhaseFailed Phase = "FAILED"
	// PhaseInterrupted means the operation was cut off (crash, timeout or
	// cancellation) and did not reach a clean terminal result. Terminal: it is
	// never re-executed automatically.
	PhaseInterrupted Phase = "INTERRUPTED"
)

// Terminal reports whether the phase is final. A terminal entry is immutable.
func (p Phase) Terminal() bool {
	return p == PhaseCompleted || p == PhaseFailed || p == PhaseInterrupted
}

// Active reports whether the phase is non-terminal (work may still be running).
func (p Phase) Active() bool { return p == PhaseReceived || p == PhaseRunning }

func (p Phase) valid() bool {
	switch p {
	case PhaseReceived, PhaseRunning, PhaseCompleted, PhaseFailed, PhaseInterrupted:
		return true
	}
	return false
}

// ResumableOperation reports whether an interrupted operation of type t may be
// safely resumed after a restart. Only VERIFY is idempotent by design — it is
// a read-only probe that can be repeated without side effects. Every mutating
// operation (CREATE_RUNTIME, NETWORK, START, STOP, REMOVE) may have applied
// partial effects before the interruption, so it is not resumable and must be
// reconciled against the actual runtime state instead.
func ResumableOperation(t string) bool {
	return t == protocol.OpVerifyHealth
}

// Entry is one persisted operation record. All fields are plain values so an
// Entry copies safely; callers never receive a pointer into the store.
type Entry struct {
	OperationID  string    `json:"operationId"`
	DeploymentID string    `json:"deploymentId"`
	Type         string    `json:"type"`
	Attempt      int       `json:"attempt"`
	Phase        Phase     `json:"phase"`
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt,omitempty"`
	Success      bool      `json:"success"`
	ErrorCode    string    `json:"errorCode,omitempty"`
	Message      string    `json:"message,omitempty"`

	// Interrupted is the recovery marker: true when the entry was transitioned
	// to INTERRUPTED rather than reaching a clean terminal phase.
	Interrupted bool `json:"interrupted,omitempty"`
	// Resumable mirrors ResumableOperation(Type) at interruption time: true
	// only for operations that are idempotent by design (VERIFY).
	Resumable bool `json:"resumable,omitempty"`
}

// Operation describes an operation to record. It is deliberately decoupled
// from protocol.Operation so the store stays a generic durable primitive.
type Operation struct {
	OperationID  string
	DeploymentID string
	Type         string
	// Attempt is the retry attempt; when zero it is parsed from OperationID.
	Attempt int
	// Phase is the initial phase. Empty defaults to RUNNING (Begin starts the
	// operation). Use RECEIVED to persist an operation that has been accepted
	// but not yet started.
	Phase Phase
}

// Result is the outcome passed to Complete.
type Result struct {
	Success    bool
	ErrorCode  string
	Message    string
	FinishedAt time.Time
}

// Errors.
var (
	// ErrNotFound is returned when an operation ID is unknown to the store.
	ErrNotFound = errors.New("state: operation not found")
	// ErrCorrupt is the sentinel wrapped by *CorruptionError. It is
	// recoverable: the store returned alongside it contains every entry that
	// could be replayed.
	ErrCorrupt = errors.New("state: corrupt state file")
)

// CorruptionError reports a CRC/parse failure while loading the state log. The
// store returned by Open is still usable and contains all records up to (and,
// for a truncated tail, through) the damaged one.
type CorruptionError struct {
	// Line is the 1-based line number of the first corrupt record.
	Line int
	// Recovered is the number of valid records applied before the failure.
	Recovered int
	// Dropped is the number of records discarded (the corrupt one and, for
	// mid-file corruption, every record after it).
	Dropped int
	// Tail is true when the corruption was the final record, i.e. an
	// incomplete append from a process that died mid-write.
	Tail bool
}

func (e *CorruptionError) Error() string {
	where := "mid-file"
	if e.Tail {
		where = "truncated tail"
	}
	return fmt.Sprintf("state: corrupt state file at line %d (%s): recovered %d entries, dropped %d",
		e.Line, where, e.Recovered, e.Dropped)
}

func (e *CorruptionError) Unwrap() error { return ErrCorrupt }

// Store is the durable operation state store. It is safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	path    string
	entries map[string]*Entry
	file    *os.File
	seq     int
	appends int

	maxLogRecords int
	log           *slog.Logger
	now           func() time.Time
}

// Option configures a Store.
type Option func(*Store)

// WithClock injects a deterministic clock (tests).
func WithClock(now func() time.Time) Option {
	return func(s *Store) {
		if now != nil {
			s.now = now
		}
	}
}

// WithLogger injects a structured logger.
func WithLogger(log *slog.Logger) Option {
	return func(s *Store) {
		if log != nil {
			s.log = log
		}
	}
}

// WithMaxLogRecords bounds the append log length before Open/Prune-style
// compaction is triggered. Non-positive disables automatic compaction.
func WithMaxLogRecords(n int) Option {
	return func(s *Store) { s.maxLogRecords = n }
}

// Open loads the state store at path. A missing file yields an empty store. An
// injectable empty path yields a memory-only store (tests).
//
// If the file is damaged, Open returns a non-nil store containing every
// recoverable entry together with an error wrapping ErrCorrupt; callers should
// log the error and continue. Only an unrecoverable I/O failure returns a nil
// store.
//
// Open also performs restart classification: entries left in RECEIVED or
// RUNNING are transitioned to INTERRUPTED (with the recovery marker) and the
// transition is persisted, so it is classified exactly once.
func Open(path string, opts ...Option) (*Store, error) {
	s := &Store{
		path:          path,
		entries:       map[string]*Entry{},
		maxLogRecords: defaultMaxLogRecords,
		log:           slog.Default(),
		now:           func() time.Time { return time.Now().UTC() },
	}
	for _, o := range opts {
		o(s)
	}
	if path == "" {
		return s, nil
	}

	loaded, corrupt, err := loadFile(path)
	if err != nil {
		return nil, err
	}
	for i := range loaded {
		e := loaded[i]
		s.entries[e.OperationID] = &e
	}
	if err := s.reopenAppendLocked(); err != nil {
		return nil, err
	}

	// Restart classification: anything still active was interrupted.
	var interrupted []Entry
	for _, e := range s.entries {
		if e.Phase.Active() {
			s.markInterruptedLocked(e, "operation interrupted by agent restart")
			interrupted = append(interrupted, *e)
		}
	}
	for i := range interrupted {
		if err := s.appendLocked(&interrupted[i]); err != nil {
			return nil, err
		}
	}

	if corrupt != nil {
		// Heal the file: rewrite a clean, compacted log atomically.
		if err := s.compactLocked(); err != nil {
			return nil, err
		}
		return s, corrupt
	}
	return s, nil
}

// Close releases the append handle. The store must not be used afterwards.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

// Begin records op. It is idempotent: if the operation ID already exists — in
// any phase, terminal or not — the existing entry is returned unchanged and the
// operation is never restarted. A new operation starts in RUNNING unless
// op.Phase requests otherwise (RECEIVED).
func (s *Store) Begin(op Operation) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if op.OperationID == "" {
		return Entry{}, errors.New("state: operationId is required")
	}
	if e, ok := s.entries[op.OperationID]; ok {
		return *e, nil
	}
	phase := op.Phase
	if phase == "" {
		phase = PhaseRunning
	}
	if !phase.valid() {
		return Entry{}, fmt.Errorf("state: invalid phase %q", phase)
	}
	if phase.Terminal() {
		return Entry{}, fmt.Errorf("state: cannot begin an operation in terminal phase %q", phase)
	}
	e := &Entry{
		OperationID:  op.OperationID,
		DeploymentID: op.DeploymentID,
		Type:         op.Type,
		Attempt:      attemptOf(op),
		Phase:        phase,
		StartedAt:    s.now(),
	}
	s.entries[e.OperationID] = e
	if err := s.appendLocked(e); err != nil {
		delete(s.entries, e.OperationID)
		return Entry{}, err
	}
	s.maybeCompactLocked()
	return *e, nil
}

// Complete records the terminal outcome of id. If the entry is already
// terminal it is returned unchanged (terminal results are never overwritten).
func (s *Store) Complete(id string, res Result) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	if e.Phase.Terminal() {
		return *e, nil
	}
	finished := res.FinishedAt
	if finished.IsZero() {
		finished = s.now()
	}
	if res.Success {
		e.Phase = PhaseCompleted
	} else {
		e.Phase = PhaseFailed
	}
	e.Success = res.Success
	e.ErrorCode = res.ErrorCode
	e.Message = res.Message
	e.FinishedAt = finished
	if err := s.appendLocked(e); err != nil {
		return *e, err
	}
	s.maybeCompactLocked()
	return *e, nil
}

// Fail records a failed terminal outcome. Terminal entries are immutable.
func (s *Store) Fail(id, code, msg string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	if e.Phase.Terminal() {
		return *e, nil
	}
	e.Phase = PhaseFailed
	e.Success = false
	e.ErrorCode = code
	e.Message = msg
	e.FinishedAt = s.now()
	if err := s.appendLocked(e); err != nil {
		return *e, err
	}
	s.maybeCompactLocked()
	return *e, nil
}

// Interrupt marks an active operation as INTERRUPTED with the recovery marker.
// Terminal entries are never downgraded.
func (s *Store) Interrupt(id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	if e.Phase.Terminal() {
		return *e, nil
	}
	s.markInterruptedLocked(e, "operation interrupted")
	if err := s.appendLocked(e); err != nil {
		return *e, err
	}
	s.maybeCompactLocked()
	return *e, nil
}

// Get returns the entry for id.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// ListActive returns the non-terminal entries, ordered deterministically by
// StartedAt then OperationID.
func (s *Store) ListActive() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.entries {
		if e.Phase.Active() {
			out = append(out, *e)
		}
	}
	sortEntries(out)
	return out
}

// ListByDeployment returns every entry for deploymentID (any phase), ordered
// deterministically by StartedAt then OperationID.
func (s *Store) ListByDeployment(deploymentID string) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.entries {
		if e.DeploymentID == deploymentID {
			out = append(out, *e)
		}
	}
	sortEntries(out)
	return out
}

// ListByPhase returns every entry in the given phase, deterministically
// ordered.
func (s *Store) ListByPhase(p Phase) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.entries {
		if e.Phase == p {
			out = append(out, *e)
		}
	}
	sortEntries(out)
	return out
}

// ListInterrupted returns every entry that was interrupted or is still active
// (defensive: an active entry means the store was not opened through Open). It
// is the input to recovery classification. Deterministically ordered.
func (s *Store) ListInterrupted() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.entries {
		if e.Interrupted || e.Phase.Active() || e.Phase == PhaseInterrupted {
			out = append(out, *e)
		}
	}
	sortEntries(out)
	return out
}

// Len returns the number of stored entries.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Prune keeps the keepLastN most recent terminal entries (by finish time,
// ties broken by OperationID) and removes older ones. Active entries are
// always kept. A negative keepLastN is a no-op; zero removes every terminal
// entry. It returns the number removed and compacts the log when anything was
// removed. Callers should run recovery before pruning so interrupted work is
// not forgotten.
func (s *Store) Prune(keepLastN int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if keepLastN < 0 {
		return 0, nil
	}
	var terminal []*Entry
	for _, e := range s.entries {
		if e.Phase.Terminal() {
			terminal = append(terminal, e)
		}
	}
	sort.Slice(terminal, func(i, j int) bool {
		ti, tj := finishTime(terminal[i]), finishTime(terminal[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return terminal[i].OperationID < terminal[j].OperationID
	})
	removed := 0
	for i := keepLastN; i < len(terminal); i++ {
		delete(s.entries, terminal[i].OperationID)
		removed++
	}
	if removed > 0 {
		if err := s.compactLocked(); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// markInterruptedLocked transitions an entry to INTERRUPTED in place.
func (s *Store) markInterruptedLocked(e *Entry, msg string) {
	e.Phase = PhaseInterrupted
	e.Interrupted = true
	e.Resumable = ResumableOperation(e.Type)
	if e.FinishedAt.IsZero() {
		e.FinishedAt = s.now()
	}
	if e.Message == "" {
		e.Message = msg
	}
}

// sortedEntriesLocked returns a deterministic snapshot of the entries.
func (s *Store) sortedEntriesLocked() []Entry {
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, *e)
	}
	sortEntries(out)
	return out
}

// sortEntries orders by StartedAt, then OperationID.
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].StartedAt.Equal(entries[j].StartedAt) {
			return entries[i].StartedAt.Before(entries[j].StartedAt)
		}
		return entries[i].OperationID < entries[j].OperationID
	})
}

func finishTime(e *Entry) time.Time {
	if !e.FinishedAt.IsZero() {
		return e.FinishedAt
	}
	return e.StartedAt
}

func attemptOf(op Operation) int {
	if op.Attempt > 0 {
		return op.Attempt
	}
	if _, _, attempt, ok := ParseOperationID(op.OperationID); ok {
		return attempt
	}
	return 0
}

var operationIDRe = regexp.MustCompile(`^op_(dep_[0-9a-f]{24})_([A-Z][A-Z_]*)_([0-9]+)$`)

// ParseOperationID splits an operation ID of the form
// op_<deploymentID>_<STEP>_<attempt> (docs/architecture/agent-protocol.md §6).
func ParseOperationID(id string) (deploymentID, step string, attempt int, ok bool) {
	m := operationIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil {
		return "", "", 0, false
	}
	return m[1], m[2], n, true
}
