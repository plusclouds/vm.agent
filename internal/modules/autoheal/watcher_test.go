package autoheal

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
)

// fakeManager lets tests script service states and count/observe restarts
// without a real systemd/SCM. Only Get and Restart matter for autoheal;
// the rest are unused stubs to satisfy the Manager interface.
type fakeManager struct {
	mu           sync.Mutex
	state        map[string]services.ServiceState
	restarts     map[string]int
	restartFails map[string]bool // if true, Restart reports ActionResult.Success=false
}

func newFakeManager() *fakeManager {
	return &fakeManager{
		state:        make(map[string]services.ServiceState),
		restarts:     make(map[string]int),
		restartFails: make(map[string]bool),
	}
}

func (f *fakeManager) setState(name string, s services.ServiceState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[name] = s
}

func (f *fakeManager) restartCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restarts[name]
}

func (f *fakeManager) Get(_ context.Context, name string) (*services.ServiceInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.state[name]
	if !ok {
		return nil, fmt.Errorf("unknown service %q", name)
	}
	return &services.ServiceInfo{Name: name, State: s}, nil
}

func (f *fakeManager) Restart(_ context.Context, name string) (*services.ActionResult, error) {
	f.mu.Lock()
	f.restarts[name]++
	fail := f.restartFails[name]
	f.mu.Unlock()
	if fail {
		return &services.ActionResult{Service: name, Action: services.ActionRestart, Success: false, Message: "boom"}, nil
	}
	// Simulate a successful restart bringing the service back to active.
	f.setState(name, services.StateActive)
	return &services.ActionResult{Service: name, Action: services.ActionRestart, Success: true}, nil
}

func (f *fakeManager) List(context.Context) ([]services.ServiceInfo, error) { return nil, nil }
func (f *fakeManager) Start(context.Context, string) (*services.ActionResult, error) {
	return nil, nil
}
func (f *fakeManager) Stop(context.Context, string) (*services.ActionResult, error) { return nil, nil }
func (f *fakeManager) Reload(context.Context, string) (*services.ActionResult, error) {
	return nil, nil
}
func (f *fakeManager) Enable(context.Context, string) (*services.ActionResult, error) {
	return nil, nil
}
func (f *fakeManager) Disable(context.Context, string) (*services.ActionResult, error) {
	return nil, nil
}

func TestCheckOne_RestartsFailedService(t *testing.T) {
	fm := newFakeManager()
	fm.setState("nginx", services.StateFailed)
	w := New(fm, []string{"nginx"}, time.Second, zap.NewNop())

	w.checkOne(context.Background(), "nginx")

	if got := fm.restartCount("nginx"); got != 1 {
		t.Errorf("restart count = %d, want 1", got)
	}
}

func TestCheckOne_IgnoresActiveAndInactive(t *testing.T) {
	fm := newFakeManager()
	fm.setState("nginx", services.StateActive)
	w := New(fm, []string{"nginx"}, time.Second, zap.NewNop())
	w.checkOne(context.Background(), "nginx")

	fm.setState("nginx", services.StateInactive)
	w.checkOne(context.Background(), "nginx")

	if got := fm.restartCount("nginx"); got != 0 {
		t.Errorf("restart count = %d, want 0 (active/inactive should never trigger a restart)", got)
	}
}

func TestCheckOne_GivesUpAfterMaxAttempts(t *testing.T) {
	fm := newFakeManager()
	fm.setState("nginx", services.StateFailed)
	fm.restartFails["nginx"] = true // every restart fails, service stays "failed"
	w := New(fm, []string{"nginx"}, time.Second, zap.NewNop())

	for i := 0; i < maxConsecutiveAttempts+3; i++ {
		w.checkOne(context.Background(), "nginx")
	}

	if got := fm.restartCount("nginx"); got != maxConsecutiveAttempts {
		t.Errorf("restart count = %d, want exactly %d (should stop trying after giving up)", got, maxConsecutiveAttempts)
	}
	if !w.hasGivenUp("nginx") {
		t.Error("expected watcher to have given up on nginx")
	}
}

func TestCheckOne_ResetsFailureCountOnceHealthy(t *testing.T) {
	fm := newFakeManager()
	fm.setState("nginx", services.StateFailed)
	w := New(fm, []string{"nginx"}, time.Second, zap.NewNop())

	// First restart succeeds (fakeManager.Restart sets state back to active).
	w.checkOne(context.Background(), "nginx")
	if got := fm.restartCount("nginx"); got != 1 {
		t.Fatalf("restart count = %d, want 1", got)
	}

	// One more check while the service reports active — this is what
	// actually clears the failure count (a poll has to observe the healthy
	// state; Restart() succeeding isn't itself the reset trigger).
	w.checkOne(context.Background(), "nginx")

	// Service fails again later — should get a fresh set of attempts, not
	// pick up where a prior failure streak left off.
	fm.setState("nginx", services.StateFailed)
	for i := 0; i < maxConsecutiveAttempts-1; i++ {
		fm.restartFails["nginx"] = true
		w.checkOne(context.Background(), "nginx")
		fm.setState("nginx", services.StateFailed) // keep it failed between attempts
	}

	if w.hasGivenUp("nginx") {
		t.Error("should not have given up yet — failure count should have reset after the earlier recovery")
	}
}

func TestCheckOne_UnknownService_LogsAndDoesNotPanic(t *testing.T) {
	fm := newFakeManager()
	w := New(fm, []string{"does-not-exist"}, time.Second, zap.NewNop())
	w.checkOne(context.Background(), "does-not-exist") // Get() errors; should just warn and return
	if got := fm.restartCount("does-not-exist"); got != 0 {
		t.Errorf("restart count = %d, want 0", got)
	}
}

func TestStart_NoWatchedServices_DoesNothing(t *testing.T) {
	fm := newFakeManager()
	w := New(fm, nil, time.Millisecond, zap.NewNop())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	w.Start(ctx) // should return immediately without starting a goroutine
	<-ctx.Done()
	// Nothing to assert beyond "did not panic and had nothing to restart".
}

func TestStart_NilWatcher_DoesNotPanic(t *testing.T) {
	var w *Watcher
	w.Start(context.Background())
}

func TestWatcher_PollsOnInterval(t *testing.T) {
	fm := newFakeManager()
	fm.setState("nginx", services.StateFailed)
	fm.restartFails["nginx"] = true
	w := New(fm, []string{"nginx"}, 10*time.Millisecond, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Millisecond)
	defer cancel()
	w.Start(ctx)
	<-ctx.Done()
	time.Sleep(10 * time.Millisecond) // let the last tick's goroutine work finish

	count := fm.restartCount("nginx")
	if count < 2 {
		t.Errorf("expected multiple poll-driven restart attempts in ~55ms at a 10ms interval, got %d", count)
	}
}
