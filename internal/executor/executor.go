// Package executor provides a safe, audited command execution utility.
// All commands are executed directly via os/exec (never through a shell),
// with full stdout/stderr capture and structured audit logging via zap.
package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

const defaultTimeout = 30 * time.Second

// Executor runs external commands safely.
type Executor struct {
	logger  *zap.Logger
	timeout time.Duration
}

// New creates an Executor with the default 30-second timeout.
func New(logger *zap.Logger) *Executor {
	return &Executor{
		logger:  logger,
		timeout: defaultTimeout,
	}
}

// NewWithTimeout creates an Executor with the specified timeout.
func NewWithTimeout(logger *zap.Logger, timeout time.Duration) *Executor {
	return &Executor{
		logger:  logger,
		timeout: timeout,
	}
}

// Execute runs the given command with the provided arguments.
// It applies a deadline to the provided context (the shorter of the context
// deadline and the executor timeout), captures stdout and stderr separately,
// and emits a structured audit log entry on every execution.
//
// The command is always invoked directly — never via sh -c — to prevent
// shell injection.
func (e *Executor) Execute(ctx context.Context, command string, args ...string) (stdout string, stderr string, err error) {
	return e.run(ctx, nil, command, args...)
}

// ExecuteWithStdin runs the given command with the provided arguments,
// writing stdin to the child process's standard input. This exists so
// sensitive values (e.g. a new password for chpasswd) can be passed to a
// command without ever appearing in argv or in the audit log below, which
// only ever records command+args, never stdin.
func (e *Executor) ExecuteWithStdin(ctx context.Context, stdin []byte, command string, args ...string) (stdout string, stderr string, err error) {
	return e.run(ctx, stdin, command, args...)
}

func (e *Executor) run(ctx context.Context, stdin []byte, command string, args ...string) (stdout string, stderr string, err error) {
	// Apply timeout: if the context already has a shorter deadline, that wins.
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	// Structured audit log — emitted for every execution regardless of outcome.
	// Deliberately never logs stdin: this is what lets ExecuteWithStdin carry
	// secrets safely.
	e.logger.Info("command executed",
		zap.String("command", command),
		zap.Strings("args", args),
		zap.Duration("duration", duration),
		zap.Int("exit_code", exitCode),
		zap.Bool("success", runErr == nil),
	)

	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return stdout, stderr, fmt.Errorf("command timed out after %s: %s", e.timeout, command)
		}
		return stdout, stderr, fmt.Errorf("command failed (exit %d): %w", exitCode, runErr)
	}

	return stdout, stderr, nil
}
