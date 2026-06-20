package domain

import "fmt"

// NotFoundError represents a missing resource.
type NotFoundError struct {
	Resource string
}

func (e NotFoundError) Error() string {
	if e.Resource == "" {
		return "not found"
	}
	return fmt.Sprintf("%s not found", e.Resource)
}

// Is enables errors.Is matching on NotFoundError.
func (e NotFoundError) Is(target error) bool {
	_, ok := target.(NotFoundError)
	if ok {
		return true
	}
	_, ok = target.(*NotFoundError)
	return ok
}

// ErrNotFound is the sentinel error for missing resources.
var ErrNotFound = NotFoundError{}

type PermissionError struct {
	Reason string
}

var ErrPermissionDenied = PermissionError{}

func (e PermissionError) Error() string {
	if e.Reason == "" {
		return "permission denied"
	}
	return fmt.Sprintf("permission denied: %s", e.Reason)
}

// Is enables errors.Is matching on PermissionError.
func (e PermissionError) Is(target error) bool {
	_, ok := target.(PermissionError)
	if ok {
		return true
	}
	_, ok = target.(*PermissionError)
	return ok
}

type RedirectError struct {
	Location string
	Body     any
}

func (e RedirectError) Error() string {
	return fmt.Sprintf("redirect to %s", e.Location)
}

func (e RedirectError) Is(target error) bool {
	_, ok := target.(RedirectError)
	if ok {
		return true
	}
	_, ok = target.(*RedirectError)
	return ok
}

var ErrRedirect = RedirectError{}

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("validation error: %s", e.Message)
	}
	return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}

// Is enables errors.Is matching on ValidationError.
func (e ValidationError) Is(target error) bool {
	_, ok := target.(ValidationError)
	if ok {
		return true
	}
	_, ok = target.(*ValidationError)
	return ok
}

var ErrValidation = ValidationError{}
