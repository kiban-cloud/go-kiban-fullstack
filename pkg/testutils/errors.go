package testutils

import (
	"errors"
	"strings"
	"testing"

	errorsWrapper "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

type stackTracer interface {
	StackTrace() errorsWrapper.StackTrace
}

// ErrorAssertions verifies err contains target (via errors.Is) and validates
// the presence (or absence) of a stack trace.
// Use when the use case wraps a sentinel: errorsWrapper.Wrap(ErrSentinel, "msg").
// Also use when the error propagates from a dependency with fmt.Errorf("%w", err).
func ErrorAssertions(t *testing.T, err error, target error, mustHaveStackTrace bool) {
	t.Helper()

	assert.Error(t, err)
	assert.True(t, errors.Is(err, target))

	var st stackTracer
	if mustHaveStackTrace {
		assert.True(t, errors.As(err, &st))
	} else {
		assert.False(t, errors.As(err, &st))
	}
}

// ErrorMessageAssertions verifies err contains the expected message and
// validates the presence (or absence) of a stack trace.
// Use when the use case creates the error directly with errorsWrapper.New("msg")
// without a sentinel.
// mustHaveStackTrace: true when the code uses errorsWrapper.New or errorsWrapper.Wrap.
func ErrorMessageAssertions(t *testing.T, err error, expectedMessage string, mustHaveStackTrace bool) {
	t.Helper()

	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), expectedMessage))

	var st stackTracer
	if mustHaveStackTrace {
		assert.True(t, errors.As(err, &st))
	} else {
		assert.False(t, errors.As(err, &st))
	}
}
