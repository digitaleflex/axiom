// Package diagnostics answers one question for an operator: why did this
// deployment fail, without SSH access (issue #102).
//
// It is strictly READ-ONLY. Nothing in this package mutates a deployment, a
// server, a plan or a configuration: when a deployment is stuck, the
// diagnostic explains the observed state, it never repairs it. Every type
// here is a pure projection of data that already exists in the authoritative
// stores (deployment records, steps, events, the redacted log journal,
// server heartbeats and the last health probe).
//
// The diagnostic is deliberately conservative about what it exposes. It
// reuses the authoritative deployment records and the already-redacted log
// journal, and it re-applies redaction on the way out (see redact.go) so a
// secret that reached a free-text field through a path the persisting
// redactor does not cover still cannot leave through this endpoint.
package diagnostics

import (
	"context"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// Bounds. Diagnostics are served to a human reading a console panel: the
// per-request cost must stay bounded, so every list is capped and every
// free-text field is truncated.
const (
	// DefaultExcerptSize is the number of log entries embedded in the
	// summary report when the caller does not ask for a specific size.
	DefaultExcerptSize = 5
	// MaxExcerptSize bounds the embedded excerpt on the summary route.
	MaxExcerptSize = 20
	// DefaultPageSize / MaxPageSize bound the paginated log route, reusing
	// the API-wide limits so the pagination semantics are identical.
	DefaultPageSize = 20
	MaxPageSize     = 100
	// maxSteps bounds how many steps a single deployment may report. A
	// canonical plan has five; the cap is a safety net against a corrupt
	// or hand-edited plan.
	maxSteps = 64
	// maxMessageRunes caps a log message or probe body rendered by the
	// diagnostic.
	maxMessageRunes = 2048
)

// StepReader reads the persisted steps of a deployment.
type StepReader interface {
	Steps(ctx context.Context, deploymentID string) ([]deployment.Step, error)
}

// ServerReader reads the record of the server a deployment targets. It is
// satisfied by the same api.ServerStore the rest of the API uses.
type ServerReader interface {
	Get(ctx context.Context, id string) (server.Record, error)
}

// LogReader reads the durable, already-redacted deployment log journal. It
// is the same paginated API the logs route exposes; diagnostics never loads
// the whole journal into memory.
type LogReader interface {
	List(ctx context.Context, deploymentID string, f logs.Filter) ([]logs.Entry, string, error)
}

// ProbeLookup returns the newest persisted health probe result for a
// deployment. It is wired to executor.LastHealthResult by the composition
// root; when nil the diagnostic reports the health section as unknown rather
// than failing.
type ProbeLookup func(ctx context.Context, deploymentID string) (health.ProbeReport, bool, error)

// Service assembles a diagnostic report from the authoritative stores.
type Service struct {
	steps  StepReader
	server ServerReader
	logs   LogReader
	probe  ProbeLookup
}

// New builds a Service. Any dependency may be nil: the corresponding
// section of the report is then reported as unavailable instead of failing
// the whole request, so a partially deployed Engine still answers.
func New(steps StepReader, servers ServerReader, logStore LogReader, probe ProbeLookup) *Service {
	return &Service{steps: steps, server: servers, logs: logStore, probe: probe}
}

// Report is the full diagnostic for one deployment. Every section is
// independently optional: an absent section means "not observable", never
// "fine".
type Report struct {
	DeploymentID  string           `json:"deploymentId"`
	Number        int              `json:"number"`
	ApplicationID string           `json:"applicationId"`
	PlanID        string           `json:"planId,omitempty"`
	Environment   string           `json:"environment,omitempty"`
	ServerID      string           `json:"serverId,omitempty"`
	Status        deployment.State `json:"status"`
	ErrorCode     string           `json:"errorCode,omitempty"`
	// Correlation is the traceability anchor an operator pastes into a
	// support request: it ties the deployment back to the API request that
	// created it and to the persisted event range.
	Correlation Correlation  `json:"correlation"`
	Timeline    Timeline     `json:"timeline"`
	Steps       []StepReport `json:"steps"`
	Server      *ServerView  `json:"server,omitempty"`
	Health      *HealthView  `json:"health,omitempty"`
	Excerpt     []LogEntry   `json:"logExcerpt"`
	ExcerptNext string       `json:"logExcerptNextCursor,omitempty"`
	// Conclusion is what the diagnostic lets the operator conclude.
	Conclusion Conclusion `json:"conclusion"`
	// Redacted documents the masking applied to this response. It is
	// always present so a client can assert the guarantee.
	Redacted RedactionNotice `json:"redacted"`
}

// Correlation carries the traceability fields of a deployment.
type Correlation struct {
	CorrelationID string `json:"correlationId,omitempty"`
	// EventSeqFrom / EventSeqTo bracket the persisted event range backing
	// this diagnostic.
	EventSeqFrom int64 `json:"eventSeqFrom,omitempty"`
	EventSeqTo   int64 `json:"eventSeqTo,omitempty"`
}

// Timeline reports when the deployment started, finished, and how long it
// took. A deployment that never started reports zero timestamps.
type Timeline struct {
	CreatedAt   time.Time  `json:"createdAt"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	// DurationMs is the elapsed time between start and completion, or
	// between start and now when the deployment is still running. nil when
	// the deployment never started.
	DurationMs *int64 `json:"durationMs,omitempty"`
	// LastEventAt is the most recent persisted event timestamp.
	LastEventAt *time.Time `json:"lastEventAt,omitempty"`
}

// StepReport is one step of the plan with its computed duration.
type StepReport struct {
	Name       string                `json:"name"`
	Position   int                   `json:"position"`
	Status     deployment.StepStatus `json:"status"`
	ErrorCode  string                `json:"errorCode,omitempty"`
	ExitCode   *int                  `json:"exitCode,omitempty"`
	StartedAt  *time.Time            `json:"startedAt,omitempty"`
	FinishedAt *time.Time            `json:"completedAt,omitempty"`
	// DurationMs is completedAt-startedAt for a finished step, and
	// now-startedAt for a running one (in which case Running is true).
	DurationMs *int64 `json:"durationMs,omitempty"`
	Running    bool   `json:"running,omitempty"`
}

// ServerView is the observed state of the target server. The address is
// deliberately NOT exposed: the diagnostic is about observability state, and
// the address is already available through GET /servers/{id}.
type ServerView struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Status       server.Status       `json:"status"`
	AgentVersion string              `json:"agentVersion,omitempty"`
	Capabilities []server.Capability `json:"capabilities"`
	CPUCount     int                 `json:"cpuCount,omitempty"`
	MemoryMB     int                 `json:"memoryMb,omitempty"`
	DiskFreeMB   int                 `json:"diskFreeMb,omitempty"`
	// LastSeenAt is the raw agent heartbeat timestamp (agent-reported).
	LastSeenAt string `json:"lastSeenAt,omitempty"`
	// Reachable is false when the server is not currently reporting as
	// ready. It is the signal that distinguishes "the deployment failed on
	// the server" from "the server is gone".
	Reachable bool `json:"reachable"`
}

// HealthView is the last persisted verification probe. Body is redacted and
// truncated before it leaves this package.
type HealthView struct {
	Status     string     `json:"status"`
	Deployment string     `json:"deploymentStatus"`
	CheckedAt  *time.Time `json:"checkedAt,omitempty"`
	Attempt    int        `json:"attempt,omitempty"`
	HTTP       *HTTPProbe `json:"http,omitempty"`
	// Body is the (redacted, truncated) probe body, omitted when absent.
	Body string `json:"body,omitempty"`
}

// HTTPProbe is the numeric part of a probe result.
type HTTPProbe struct {
	StatusCode int   `json:"statusCode"`
	LatencyMs  int64 `json:"latencyMs"`
}

// LogEntry is one journal line rendered by the diagnostic. Message is
// redacted and truncated.
type LogEntry struct {
	ID         string      `json:"id"`
	OccurredAt time.Time   `json:"occurredAt"`
	Level      logs.Level  `json:"level"`
	Step       logs.Step   `json:"step,omitempty"`
	Source     logs.Source `json:"source,omitempty"`
	Message    string      `json:"message"`
}

// RedactionNotice documents, in the response itself, which fields were
// masked. It lets a client (and a test) assert the guarantee without
// scraping for secrets.
type RedactionNotice struct {
	Applied bool     `json:"applied"`
	Fields  []string `json:"fields"`
	Reason  string   `json:"reason"`
}

// redactionNotice is the fixed notice returned by every diagnostic response.
func redactionNotice() RedactionNotice {
	return RedactionNotice{
		Applied: true,
		Fields: []string{
			"health.body",
			"logExcerpt[].message",
			"logs[].message",
			"server.address",
			"agent.credentials",
			"agent.headers",
		},
		Reason: "diagnostics are read-only and never return credentials, tokens, " +
			"authentication headers or configuration values",
	}
}

// Conclusion is the operator-facing verdict: a stable code, a severity, a
// human summary and the ordered findings that justify it.
type Conclusion struct {
	// Severity is ERROR for a real failure, WARN for a cancelled or
	// degraded deployment, INFO for a running or healthy one.
	Severity logs.Level `json:"severity"`
	// Code is the stable machine-readable verdict, one of the Conclusion*X
	// constants. Clients branch on it, never on Summary.
	Code     string    `json:"code"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// Stable conclusion codes. These are contract: a console may map them to a
// remediation hint, so they must not change meaning.
const (
	// ConclusionLive: the deployment reached LIVE and verified.
	ConclusionLive = "LIVE"
	// ConclusionCancelled: an operator cancelled the deployment.
	ConclusionCancelled = "CANCELLED"
	// ConclusionStepFailed: a specific plan step failed.
	ConclusionStepFailed = "STEP_FAILED"
	// ConclusionErrorCode: the deployment failed at deployment level with a
	// stable error code but no failed step.
	ConclusionErrorCode = "DEPLOYMENT_ERROR_CODE"
	// ConclusionIncomplete: FAILED with neither a failed step nor an error
	// code — the record is not attributable.
	ConclusionIncomplete = "INCOMPLETE_RECORD"
	// ConclusionInProgress: the deployment has not failed and is not finished.
	ConclusionInProgress = "IN_PROGRESS"
)

// Finding is one piece of evidence behind the conclusion.
type Finding struct {
	// Code is a stable machine-readable finding identifier.
	Code    string `json:"code"`
	Message string `json:"message"`
}
