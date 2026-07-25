// Package autoheal watches an explicit list of services and restarts any
// that enter the failed state, with a per-service circuit breaker so a
// service that's failing for a persistent reason (bad config, missing
// dependency) doesn't get restarted forever.
package autoheal

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
)

// maxConsecutiveAttempts is how many restart attempts a service gets before
// autoheal gives up on it and waits for it to be seen healthy again (or for
// a human to intervene) before trying again.
const maxConsecutiveAttempts = 5

// Watcher periodically checks a fixed list of services and restarts any
// found in the failed state.
type Watcher struct {
	svc      services.Manager
	logger   *zap.Logger
	watched  []string
	interval time.Duration

	mu       sync.Mutex
	failures map[string]int // consecutive restart-attempt count per service
	gaveUp   map[string]bool
}

// New returns a Watcher for the given services. It does nothing until
// Start is called, and Start itself is a no-op if watched is empty.
func New(svc services.Manager, watched []string, interval time.Duration, logger *zap.Logger) *Watcher {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Watcher{
		svc:      svc,
		logger:   logger,
		watched:  watched,
		interval: interval,
		failures: make(map[string]int),
		gaveUp:   make(map[string]bool),
	}
}

// Start launches the watch loop in a goroutine. It runs until ctx is
// cancelled. A nil Watcher or one with no watched services does nothing.
func (w *Watcher) Start(ctx context.Context) {
	if w == nil || len(w.watched) == 0 {
		return
	}
	w.logger.Info("autoheal watching services",
		zap.Strings("services", w.watched),
		zap.Duration("interval", w.interval),
		zap.Int("max_attempts", maxConsecutiveAttempts),
	)
	go w.loop(ctx)
}

func (w *Watcher) loop(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.checkAll(ctx)
		}
	}
}

func (w *Watcher) checkAll(ctx context.Context) {
	for _, name := range w.watched {
		w.checkOne(ctx, name)
	}
}

func (w *Watcher) checkOne(ctx context.Context, name string) {
	info, err := w.svc.Get(ctx, name)
	if err != nil {
		w.logger.Warn("autoheal: could not query service state", zap.String("service", name), zap.Error(err))
		return
	}

	if info.State == services.StateActive {
		w.reset(name)
		return
	}
	if info.State != services.StateFailed {
		return
	}

	if w.hasGivenUp(name) {
		return
	}

	attempts := w.recordAttempt(name)
	result, err := w.svc.Restart(ctx, name)

	switch {
	case err != nil:
		w.logger.Warn("autoheal: restart attempt errored",
			zap.String("service", name), zap.Int("attempt", attempts), zap.Error(err))
	case result != nil && !result.Success:
		w.logger.Warn("autoheal: restart attempt failed",
			zap.String("service", name), zap.Int("attempt", attempts), zap.String("message", result.Message))
	default:
		w.logger.Info("autoheal: restarted failed service",
			zap.String("service", name), zap.Int("attempt", attempts))
	}

	if attempts >= maxConsecutiveAttempts {
		w.giveUp(name)
		w.logger.Error("autoheal: giving up on service after repeated failures",
			zap.String("service", name), zap.Int("attempts", attempts))
	}
}

func (w *Watcher) recordAttempt(name string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failures[name]++
	return w.failures[name]
}

func (w *Watcher) hasGivenUp(name string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gaveUp[name]
}

func (w *Watcher) giveUp(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gaveUp[name] = true
}

// reset clears a service's failure count once it's seen active again,
// re-arming autoheal for it — a service that recovers and later fails
// again gets a fresh set of attempts rather than staying given-up forever.
func (w *Watcher) reset(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failures[name] == 0 && !w.gaveUp[name] {
		return
	}
	delete(w.failures, name)
	delete(w.gaveUp, name)
}
