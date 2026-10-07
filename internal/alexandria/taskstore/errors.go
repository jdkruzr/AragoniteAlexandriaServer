package taskstore

import "errors"

// ErrNotFound is returned when a task is not found.
var ErrNotFound = errors.New("task not found")

// IsNotFound returns true if the error is ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
