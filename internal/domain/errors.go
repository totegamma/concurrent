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
