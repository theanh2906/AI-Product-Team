package agentstream

import (
	"context"
	"strings"
)

// Event is a provider-neutral update emitted while an AI agent is working.
type Event struct {
	Level   string
	Kind    string
	Message string
	Detail  string
}

// Reporter receives normalized runtime events for one task execution.
type Reporter interface {
	Report(Event)
}

type reporterContextKey struct{}

// WithReporter binds a task-scoped reporter to the worker context.
func WithReporter(ctx context.Context, reporter Reporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterContextKey{}, reporter)
}

// Emit reports an event when the current worker has a live session attached.
func Emit(ctx context.Context, event Event) {
	reporter := FromContext(ctx)
	if reporter == nil || strings.TrimSpace(event.Message) == "" {
		return
	}
	reporter.Report(event)
}

// FromContext returns the reporter attached to a worker context, if any.
func FromContext(ctx context.Context) Reporter {
	reporter, _ := ctx.Value(reporterContextKey{}).(Reporter)
	return reporter
}
