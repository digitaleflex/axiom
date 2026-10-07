package api

import (
	"encoding/json"
	"errors"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// handlerFunc is an API handler that returns an error instead of writing it.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func (a *API) wrap(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			a.writeError(w, r, err)
		}
	}
}

func (a *API) writeError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := fromDomain(err)
	if apiErr.Status >= 500 {
		// Server-side detail only, with secrets redacted: the message is
		// never returned to the client (stable INTERNAL_ERROR instead).
		a.log.Error("request failed", "requestId", requestID(r.Context()), "method", r.Method, "path", r.URL.Path, "error", logs.Redact(err.Error()))
	}
	details := apiErr.Details
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, apiErr.Status, map[string]any{"error": map[string]any{
		"code": apiErr.Code, "message": apiErr.Message, "requestId": requestID(r.Context()), "details": details,
	}})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().Debug("write response", "error", err)
	}
}

// decodeJSON strictly decodes a JSON object body: correct content type,
// bounded size, no unknown fields, no trailing data.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "application/json" {
		return newError(http.StatusUnsupportedMediaType, CodeInvalidRequest, "Content-Type must be application/json", nil)
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return newError(http.StatusRequestEntityTooLarge, CodeInvalidRequest, "request body is too large", nil)
		case strings.HasPrefix(err.Error(), "json: unknown field"):
			return errInvalid(strings.TrimPrefix(err.Error(), "json: "))
		case errors.Is(err, io.EOF):
			return errInvalid("request body is required")
		default:
			return errInvalid("request body must be a valid JSON object")
		}
	}
	if dec.More() {
		return errInvalid("request body must contain a single JSON object")
	}
	return nil
}

// page holds validated pagination parameters (API §21).
type page struct{ Page, Limit int }

func (p page) offset() int { return (p.Page - 1) * p.Limit }

const (
	defaultLimit = 20
	maxLimit     = 100
)

func parsePage(r *http.Request) (page, error) {
	p := page{Page: 1, Limit: defaultLimit}
	q := r.URL.Query()
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return p, errInvalid("page must be a positive integer")
		}
		p.Page = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			return p, errInvalid("limit must be an integer between 1 and 100")
		}
		p.Limit = n
	}
	return p, nil
}

func pageResponse[T any](items []T, p page, total int) map[string]any {
	if items == nil {
		items = []T{}
	}
	return map[string]any{"items": items, "page": p.Page, "limit": p.Limit, "total": total}
}

// audit records privileged operations: actor, action, target, result,
// request ID and timestamp (structured log; queryable trail in #128).
// Secrets must never appear in action or target strings.
func (a *API) audit(r *http.Request, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	a.log.Info("audit", "requestId", requestID(r.Context()), "actor", principal(r.Context()).UserID,
		"action", action, "target", target, "result", result)
}
