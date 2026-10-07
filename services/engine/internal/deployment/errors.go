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

var (
	// ErrPlanNotFound is returned when the plan does not exist for the application.
	ErrPlanNotFound = errors.New("deployment plan not found for application")
	// ErrPlanAlreadyUsed is returned when a plan already produced a deployment (plans are single-use).
	ErrPlanAlreadyUsed = errors.New("deployment plan already used")
	// ErrStepNotFound is returned when the step is not part of the deployment plan.
	ErrStepNotFound = errors.New("deployment step not found")
)
