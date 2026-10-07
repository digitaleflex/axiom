package api

import (
	"errors"
	"net/http"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	ghauth "github.com/digitaleflex/axiom/services/engine/internal/github/auth"
	"github.com/digitaleflex/axiom/services/engine/internal/github/repos"
)

// Canonical error codes (docs/architecture/api-contract.md §18).
// Clients branch on the code, never on the message.
const (
	CodeInvalidRequest         = "INVALID_REQUEST"
	CodeUnauthorized           = "UNAUTHORIZED"
	CodeForbidden              = "FORBIDDEN"
	CodeNotFound               = "NOT_FOUND"
	CodeConflict               = "CONFLICT"
	CodeRateLimited            = "RATE_LIMITED"
	CodeValidationFailed       = "VALIDATION_FAILED"
	CodeDeploymentNotEligible  = "DEPLOYMENT_NOT_ELIGIBLE"
	CodeDeploymentInvalidState = "DEPLOYMENT_INVALID_STATE"
	CodePolicyDenied           = "POLICY_DENIED"
	CodeInternalError          = "INTERNAL_ERROR"
	CodeServiceUnavailable     = "SERVICE_UNAVAILABLE"
)

// Error is an API error rendered with the stable envelope.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newError(status int, code, message string, details map[string]any) *Error {
	return &Error{Status: status, Code: code, Message: message, Details: details}
}

func errInvalid(message string) *Error {
	return newError(http.StatusBadRequest, CodeInvalidRequest, message, nil)
}

func errValidation(message string, details map[string]any) *Error {
	return newError(http.StatusUnprocessableEntity, CodeValidationFailed, message, details)
}

func errNotFound(resource, id string) *Error {
	return newError(http.StatusNotFound, CodeNotFound, resource+" not found", map[string]any{resource + "Id": id})
}

var errUnavailable = newError(http.StatusServiceUnavailable, CodeServiceUnavailable, "a required dependency is unavailable", nil)

// fromDomain maps domain errors to API errors. Unknown errors become 500
// without leaking internal messages.
func fromDomain(err error) *Error {
	var apiErr *Error
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case isAnalysisErr(err):
		e, _ := analysisError(err)
		return e
	case errors.Is(err, deployment.ErrNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "deployment not found", nil)
	case errors.Is(err, deployment.ErrPlanNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "deployment plan not found for this application", nil)
	case errors.Is(err, deployment.ErrPlanAlreadyUsed):
		return newError(http.StatusConflict, CodeConflict, "this deployment plan has already been used; generate a new plan", nil)
	case errors.Is(err, deployment.ErrIdempotencyConflict):
		return newError(http.StatusConflict, CodeConflict, "Idempotency-Key was already used for a different request", nil)
	case errors.Is(err, deployment.ErrInvalidTransition):
		return newError(http.StatusConflict, CodeDeploymentInvalidState, "the deployment cannot perform this action in its current state", nil)
	case errors.Is(err, repos.ErrRefNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "ref not found", nil)
	case errors.Is(err, repos.ErrNotFound), errors.Is(err, repos.ErrConnectionAbsent), errors.Is(err, ghauth.ErrNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "GitHub connection or repository not found", nil)
	case errors.Is(err, repos.ErrInvalidRef):
		return newError(http.StatusBadRequest, CodeInvalidRequest, "invalid ref name", nil)
	case errors.Is(err, repos.ErrReconnectNeeded), errors.Is(err, ghauth.ErrDisconnected), errors.Is(err, ghauth.ErrNeedsAttention):
		return newError(http.StatusConflict, CodeConflict, "GitHub must be reconnected", map[string]any{"reason": "github_reconnect_required"})
	case errors.Is(err, repos.ErrForbidden):
		return newError(http.StatusForbidden, CodeForbidden, "GitHub denied access to this resource", nil)
	case errors.Is(err, repos.ErrRateLimited):
		return newError(http.StatusTooManyRequests, CodeRateLimited, "GitHub rate limit reached; retry later", nil)
	case errors.Is(err, repos.ErrUnavailable), errors.Is(err, ghauth.ErrProvider):
		return newError(http.StatusServiceUnavailable, CodeServiceUnavailable, "GitHub is unavailable; retry later", nil)
	case errors.Is(err, deployment.ErrInvalidInput):
		return newError(http.StatusBadRequest, CodeInvalidRequest, err.Error(), nil)
	default:
		return newError(http.StatusInternalServerError, CodeInternalError, "the Engine could not complete the request", nil)
	}
}

func isAnalysisErr(err error) bool { _, ok := analysisError(err); return ok }
