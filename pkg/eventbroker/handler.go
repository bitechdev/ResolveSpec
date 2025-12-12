package eventbroker

import "context"

// EventHandler processes an event
type EventHandler interface {
	Handle(ctx context.Context, event *Event) error
}

// EventHandlerFunc is a function adapter for EventHandler
// This allows using regular functions as event handlers
type EventHandlerFunc func(ctx context.Context, event *Event) error

// Handle implements EventHandler
func (f EventHandlerFunc) Handle(ctx context.Context, event *Event) error {
	return f(ctx, event)
}
