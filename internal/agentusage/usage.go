package agentusage

import (
	"context"
	"sync"
)

type Usage struct {
	InputTokens           int64
	CachedInputTokens     int64
	CacheWriteInputTokens int64
	OutputTokens          int64
	ReasoningTokens       int64
	TotalTokens           int64
	CostUSD               float64
	Samples               int
	Partial               bool
}

type Collector struct {
	mu    sync.Mutex
	usage Usage
}
type contextKey struct{}

func WithCollector(ctx context.Context) (context.Context, *Collector) {
	collector := &Collector{}
	return context.WithValue(ctx, contextKey{}, collector), collector
}

func FromContext(ctx context.Context) *Collector {
	collector, _ := ctx.Value(contextKey{}).(*Collector)
	return collector
}

func Record(ctx context.Context, usage Usage) {
	if collector := FromContext(ctx); collector != nil {
		collector.Add(usage)
	}
}

func (c *Collector) Add(usage Usage) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.usage.InputTokens += usage.InputTokens
	c.usage.CachedInputTokens += usage.CachedInputTokens
	c.usage.CacheWriteInputTokens += usage.CacheWriteInputTokens
	c.usage.OutputTokens += usage.OutputTokens
	c.usage.ReasoningTokens += usage.ReasoningTokens
	c.usage.TotalTokens += usage.TotalTokens
	c.usage.CostUSD += usage.CostUSD
	c.usage.Samples += max(1, usage.Samples)
	c.usage.Partial = c.usage.Partial || usage.Partial
}

func (c *Collector) Snapshot() Usage {
	if c == nil {
		return Usage{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usage
}
