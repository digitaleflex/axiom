// Package logs provides durable, queryable deployment logs (issue #66):
// levelled entries correlated to a deployment step and source, with secret
// redaction applied before persistence and a bounded per-deployment retention.
package logs

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

// Levels (API contract §14).
type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

// Steps correlate an entry to a deployment step (API contract §13). The zero
// value "" is valid and means "not step-correlated".
type Step string

const (
	StepBuild         Step = "BUILD"
	StepCreateRuntime Step = "CREATE_RUNTIME"
	StepNetwork       Step = "NETWORK"
	StepStart         Step = "START"
	StepVerify        Step = "VERIFY"
)

// Sources correlate an entry to the subsystem that produced it.
type Source string

const (
	SourceBuild   Source = "build"
	SourceDeploy  Source = "deploy"
	SourceRuntime Source = "runtime"
)

// Entry is one persisted deployment log line. ID and OccurredAt may be left
// zero; the store assigns an ID and the current time.
type Entry struct {
	ID           string    `json:"id"`
	DeploymentID string    `json:"deploymentId"`
	OccurredAt   time.Time `json:"occurredAt"`
	Level        Level     `json:"level"`
	Step         Step      `json:"step"`
	Source       Source    `json:"source"`
	Message      string    `json:"message"`
}

// Filter selects entries for List. Zero values match everything.
type Filter struct {
	Levels   []Level // exact set of levels; empty = all levels
	MinLevel Level   // minimum severity; ignored when Levels is set
	Step     Step    // empty = all steps
	Source   Source  // empty = all sources
	Search   string  // case-insensitive substring of Message; empty = none
	Cursor   string  // ID of the last entry of the previous page; empty = first page
	Limit    int     // page size; 0 → default (100), max 1000
}

// Store persists and queries deployment logs. Implementations must redact
// secrets in Append before writing and enforce the retention cap.
type Store interface {
	// Append persists entries in a single transaction, redacting secrets and
	// trimming the deployment's history to the configured cap.
	Append(ctx context.Context, entries ...Entry) error
	// List returns entries for one deployment ordered by (occurred_at, id).
	// nextCursor is the ID to pass as Filter.Cursor for the next page, or ""
	// when the last page was reached.
	List(ctx context.Context, deploymentID string, f Filter) ([]Entry, string, error)
	// Count returns the number of stored entries for one deployment.
	Count(ctx context.Context, deploymentID string) (int, error)
	// Trim deletes the oldest entries of one deployment beyond keepLastN and
	// returns the number of deleted rows.
	Trim(ctx context.Context, deploymentID string, keepLastN int) (int, error)
}

// Appender is the minimal persistence boundary other packages (build) depend
// on; it is satisfied by Store.
type Appender interface {
	Append(ctx context.Context, entries ...Entry) error
}

// NewID returns an opaque log identifier (API contract §3). The leading hex
// is the Unix nanosecond time so lexicographic ID order matches OccurredAt
// order, which keeps cursor pagination stable.
func NewID() string {
	var b [14]byte
	binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
	if _, err := rand.Read(b[8:]); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return "log_" + hex.EncodeToString(b[:])
}

// levelRank orders levels by severity so MinLevel can expand to a level set.
func levelRank(l Level) int {
	switch l {
	case LevelDebug:
		return 0
	case LevelInfo:
		return 1
	case LevelWarn:
		return 2
	case LevelError:
		return 3
	default:
		return -1
	}
}

// levelsAtLeast returns every level at or above min, in severity order.
func levelsAtLeast(min Level) ([]Level, error) {
	if levelRank(min) < 0 {
		return nil, fmt.Errorf("unknown log level %q", string(min))
	}
	all := []Level{LevelDebug, LevelInfo, LevelWarn, LevelError}
	var out []Level
	for _, l := range all {
		if levelRank(l) >= levelRank(min) {
			out = append(out, l)
		}
	}
	return out, nil
}
