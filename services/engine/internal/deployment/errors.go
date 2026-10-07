package deployment

import "errors"

var (
	// ErrInvalidTransition is returned when a state change violates the state machine.
	ErrInvalidTransition = errors.New("invalid deployment transition")
	// ErrNotFound is returned when a deployment does not exist.
	ErrNotFound = errors.New("deployment not found")
	// ErrIdempotencyConflict is returned when an idempotency key is reused with a different request.
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
)
