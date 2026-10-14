package booxpage

import "errors"

// ErrUnavailable marks a preview that failed because a page file is missing
// or unreadable right now. It may arrive later, so callers retry; any other
// preview error is a property of the page's data and won't change by waiting.
var ErrUnavailable = errors.New("page files are not all available")

type unavailableError string

func (e unavailableError) Error() string        { return string(e) }
func (e unavailableError) Is(target error) bool { return target == ErrUnavailable }

func unavailable(msg string) error { return unavailableError(msg) }
