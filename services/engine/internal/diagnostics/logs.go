package diagnostics

import (
	"context"
	"fmt"

	"github.com/digitaleflex/axiom/services/engine/internal/logs"
)

// LogQuery selects one page of the diagnostic log view. It is a strict
// subset of the existing logs.Filter semantics so the diagnostic log route
// paginates exactly like GET /deployments/{id}/logs, and the same cursor can
// be used against either endpoint.
type LogQuery struct {
	// Level is a minimum severity (debug, info, warn, error). Empty = all.
	Level string
	// Step restricts to a canonical plan step. Empty = all.
	Step string
	// Source restricts to a subsystem (build, deploy, runtime). Empty = all.
	Source string
	// Search is a case-insensitive substring of the message.
	Search string
	// Cursor is the ID of the last entry of the previous page.
	Cursor string
	// Limit is the page size; 0 selects DefaultPageSize. Values above
	// MaxPageSize are rejected by the caller with INVALID_REQUEST.
	Limit int
}

// LogPage is one bounded page of the diagnostic log view.
type LogPage struct {
	Items      []LogEntry `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
	// Total is not computed: counting the whole journal would defeat the
	// bounded read. Clients page until NextCursor is absent.
	Redacted RedactionNotice `json:"redacted"`
}

// Logs returns one bounded, redacted page of the deployment journal. The
// read is delegated to the same paginated store the logs route uses, so a
// diagnostic never loads the whole journal into memory.
func (s *Service) Logs(ctx context.Context, deploymentID string, q LogQuery) (*LogPage, error) {
	if s.logs == nil {
		return nil, ErrLogsUnavailable
	}
	f, err := q.filter()
	if err != nil {
		return nil, err
	}
	entries, next, err := s.logs.List(ctx, deploymentID, f)
	if err != nil {
		return nil, err
	}
	items := make([]LogEntry, 0, len(entries))
	for _, e := range entries {
		items = append(items, LogEntry{
			ID: e.ID, OccurredAt: e.OccurredAt, Level: e.Level,
			Step: e.Step, Source: e.Source, Message: safeMessage(e.Message),
		})
	}
	return &LogPage{Items: items, NextCursor: next, Redacted: redactionNotice()}, nil
}

// filter validates the query and maps it onto the journal's filter. It
// rejects unknown enum values instead of silently dropping the predicate,
// which would hand the operator a page that is not what they asked for.
func (q LogQuery) filter() (logs.Filter, error) {
	f := logs.Filter{Search: q.Search, Cursor: q.Cursor, Limit: q.Limit}
	if f.Limit == 0 {
		f.Limit = DefaultPageSize
	}
	if f.Limit < 1 || f.Limit > MaxPageSize {
		return f, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidQuery, MaxPageSize)
	}
	if q.Level != "" {
		lv := logs.Level(upper(q.Level))
		switch lv {
		case logs.LevelDebug, logs.LevelInfo, logs.LevelWarn, logs.LevelError:
			f.MinLevel = lv
		default:
			return f, fmt.Errorf("%w: level must be debug, info, warn or error", ErrInvalidQuery)
		}
	}
	if q.Step != "" {
		st, ok := canonicalLogStep(q.Step)
		if !ok {
			return f, fmt.Errorf("%w: step must be a canonical plan step", ErrInvalidQuery)
		}
		f.Step = st
	}
	if q.Source != "" {
		src := logs.Source(lower(q.Source))
		switch src {
		case logs.SourceBuild, logs.SourceDeploy, logs.SourceRuntime:
			f.Source = src
		default:
			return f, fmt.Errorf("%w: source must be build, deploy or runtime", ErrInvalidQuery)
		}
	}
	return f, nil
}
