package errors_domain

import "errors"

var (
	ErrDocumentNotFound          = errors.New("document not found")
	ErrMismatchedHashAndPassword = errors.New("mismatched hash and password")
	ErrDuplicateKey              = errors.New("duplicate key error")
	ErrNotAuthorized             = errors.New("not authorized")
)
