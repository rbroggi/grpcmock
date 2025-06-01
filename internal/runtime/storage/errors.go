package storage

import "errors"

var (
	// ErrNotFound indicates that the requested item (e.g., expectation) was not found.
	ErrNotFound = errors.New("item not found")
	// ErrAlreadyExists indicates that an item with the same identifier already exists.
	ErrAlreadyExists = errors.New("item already exists")
	// ErrInvalidInput indicates that the input provided was invalid.
	ErrInvalidInput = errors.New("invalid input")
)
