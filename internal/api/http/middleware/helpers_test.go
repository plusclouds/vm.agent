package middleware

import (
	"testing"

	"go.uber.org/zap"
)

// zapNoop returns a no-op zap logger suitable for use in tests.
func zapNoop(t *testing.T) *zap.Logger {
	t.Helper()
	return zap.NewNop()
}
