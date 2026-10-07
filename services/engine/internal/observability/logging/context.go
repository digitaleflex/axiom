package logging

import "context"

// Context keys are unexported: callers set and read identifiers through the
// With*/extract helpers so the storage layout stays an implementation detail
// and the API's own middleware context keys remain independent.
type ctxKey int

const (
	requestIDKey ctxKey = iota
	correlationIDKey
	deploymentIDKey
)

// Stable JSON field names shared by every Engine log line. They are part of
// the operational contract (docs/architecture/logging.md) and must not be
// renamed without a migration note.
const (
	FieldRequestID     = "requestId"
	FieldCorrelationID = "correlationId"
	FieldDeploymentID  = "deploymentId"
	FieldService       = "service"
	FieldComponent     = "component"
	FieldOperation     = "operation"
	FieldDurationMs    = "durationMs"
	FieldResult        = "result"
)

// WithRequestID returns ctx carrying the API request identifier
// (X-Request-ID, generated when absent).
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// WithCorrelationID returns ctx carrying the execution correlation identifier
// (req_…), the value propagated to Agent operations and protocol messages.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// WithDeploymentID returns ctx carrying the deployment identifier.
func WithDeploymentID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, deploymentIDKey, id)
}

// RequestID extracts the request identifier, or "" when absent.
func RequestID(ctx context.Context) string { return stringValue(ctx, requestIDKey) }

// CorrelationID extracts the correlation identifier, or "" when absent.
func CorrelationID(ctx context.Context) string { return stringValue(ctx, correlationIDKey) }

// DeploymentID extracts the deployment identifier, or "" when absent.
func DeploymentID(ctx context.Context) string { return stringValue(ctx, deploymentIDKey) }

func stringValue(ctx context.Context, key ctxKey) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(key).(string)
	return v
}

// Attrs extracts the correlation fields present in ctx as slog key/value
// pairs, in a stable order: requestId, correlationId, deploymentId. Absent or
// empty identifiers are omitted so log lines stay compact. Attrs is
// middleware-agnostic: it reads only the keys this package writes, never HTTP
// headers or request state.
func Attrs(ctx context.Context) []any {
	if ctx == nil {
		return nil
	}
	var attrs []any
	if v := RequestID(ctx); v != "" {
		attrs = append(attrs, FieldRequestID, v)
	}
	if v := CorrelationID(ctx); v != "" {
		attrs = append(attrs, FieldCorrelationID, v)
	}
	if v := DeploymentID(ctx); v != "" {
		attrs = append(attrs, FieldDeploymentID, v)
	}
	return attrs
}
