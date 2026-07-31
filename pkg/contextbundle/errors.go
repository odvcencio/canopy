package contextbundle

import "errors"

// ErrRequiredEvidenceExceedsBudget is returned when required items alone
// exceed the requested total budget, carrying the minimum budget that would
// have fit them (spec 10.7).
type ErrRequiredEvidenceExceedsBudget struct {
	MinimumTokens int
	RequestTokens int
}

func (e *ErrRequiredEvidenceExceedsBudget) Error() string {
	return "required evidence exceeds requested budget"
}

// Is allows errors.Is(err, ErrRequiredEvidenceExceedsBudgetSentinel) style
// checks without callers needing the concrete type.
func (e *ErrRequiredEvidenceExceedsBudget) Is(target error) bool {
	_, ok := target.(*ErrRequiredEvidenceExceedsBudget)
	return ok
}

// ErrIndexNotFound is returned when no structural index exists for a root and
// the provider was not able to build one.
var ErrIndexNotFound = errors.New("contextbundle: no index available for root")

// ErrEmptyRoot is returned when a request omits a workspace root.
var ErrEmptyRoot = errors.New("contextbundle: request root is required")
