package taskstore

import "errors"

// ErrNotFound is returned when a task is not found.
var ErrNotFound = errors.New("task not found")

// IsNotFound returns true if the error is ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// ErrDeleted is returned when a write targets a task in the trash. Restore it
// first; a client can't recreate it by reusing its UID.
var ErrDeleted = errors.New("task is deleted")

// ErrPurged is returned when a write targets a permanently deleted task. Its
// UID stays reserved so a device that missed the deletion can't recreate it.
var ErrPurged = errors.New("task was permanently deleted")
