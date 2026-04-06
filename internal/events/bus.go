// Package events provides a lightweight in-process publish/subscribe event bus.
// It is used to decouple agent modules: for example, the services module can
// publish a ServiceFailed event that the autoheal module subscribes to.
package events

import (
	"sync"
	"time"
)

// Event type constants used by all agent modules.
const (
	// ServiceStarted is published when a managed service transitions to active.
	ServiceStarted = "ServiceStarted"
	// ServiceStopped is published when a managed service transitions to inactive.
	ServiceStopped = "ServiceStopped"
	// ServiceFailed is published when a managed service enters the failed state.
	ServiceFailed = "ServiceFailed"
	// MetricThresholdExceeded is published when a resource metric crosses a limit.
	MetricThresholdExceeded = "MetricThresholdExceeded"
	// AgentRegistered is published after the agent successfully registers with
	// the PlusClouds control plane.
	AgentRegistered = "AgentRegistered"
	// ConfigDriftDetected is published when the running configuration differs
	// from the desired state in the control plane.
	ConfigDriftDetected = "ConfigDriftDetected"
)

// Event carries a typed payload from a publisher to one or more subscribers.
type Event struct {
	// Type identifies the kind of event (use the constants above).
	Type string
	// Payload holds the event-specific data. Subscribers must type-assert
	// this to the appropriate concrete type.
	Payload interface{}
	// Timestamp is when the event was published (UTC).
	Timestamp time.Time
}

// handler is the subscriber callback signature.
type handler func(Event)

// Bus is a thread-safe in-process publish/subscribe event bus.
// Zero value is not valid; use NewBus() to create one.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]handler
}

// NewBus creates and returns a ready-to-use event bus.
func NewBus() *Bus {
	return &Bus{
		handlers: make(map[string][]handler),
	}
}

// Subscribe registers h to be called every time an event of the given type
// is published. Multiple handlers may be registered for the same event type;
// they are called in registration order.
//
// h is called synchronously from the goroutine that calls Publish, so
// handlers should be fast. Long-running work should be dispatched to a
// goroutine inside the handler.
func (b *Bus) Subscribe(eventType string, h func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], h)
}

// Publish sends an event to all subscribers registered for eventType.
// It is safe to call from multiple goroutines concurrently.
func (b *Bus) Publish(eventType string, payload interface{}) {
	b.mu.RLock()
	handlers := make([]handler, len(b.handlers[eventType]))
	copy(handlers, b.handlers[eventType])
	b.mu.RUnlock()

	ev := Event{
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	}

	for _, h := range handlers {
		h(ev)
	}
}

// Unsubscribe removes all handlers for the given event type.
// This is primarily useful in tests where the bus is shared between test cases.
func (b *Bus) Unsubscribe(eventType string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.handlers, eventType)
}
