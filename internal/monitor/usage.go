package monitor

import (
    "fmt"
    "strings"
    "sync"
    "time"
)

type UsageRecord struct {
    Agent        string
    Model        string
    InputTokens  int
    OutputTokens int
    EstimatedUSD float64
    DurationMs   int64
}

type Pricing struct {
    InputPer1M  float64
    OutputPer1M float64
}

var PricingTable = map[string]Pricing{
    "gpt-4o-mini": {
        InputPer1M:  0.15,
        OutputPer1M: 0.60,
    },
    "gpt-4.1": {
        InputPer1M:  2.00,
        OutputPer1M: 8.00,
    },
    "claude-sonnet-4": {
        InputPer1M:  3.00,
        OutputPer1M: 15.00,
    },
    "gemini-2.5-flash": {
        InputPer1M:  0.30,
        OutputPer1M: 2.50,
    },
}

var (
    records []UsageRecord
    mu      sync.Mutex
)

func EstimateTokens(text string) int {
    if text == "" {
        return 0
    }

    return len([]rune(text)) / 4
}

func EstimateCost(model string, inputTokens, outputTokens int) float64 {
    pricing, ok := PricingTable[strings.ToLower(model)]
    if !ok {
        return 0
    }

    inputCost := (float64(inputTokens) / 1_000_000) * pricing.InputPer1M
    outputCost := (float64(outputTokens) / 1_000_000) * pricing.OutputPer1M

    return inputCost + outputCost
}

func Record(agent, model, input, output string, startedAt time.Time) UsageRecord {
    inputTokens := EstimateTokens(input)
    outputTokens := EstimateTokens(output)

    record := UsageRecord{
        Agent:        agent,
        Model:        model,
        InputTokens:  inputTokens,
        OutputTokens: outputTokens,
        EstimatedUSD: EstimateCost(model, inputTokens, outputTokens),
        DurationMs:   time.Since(startedAt).Milliseconds(),
    }

    mu.Lock()
    defer mu.Unlock()

    records = append(records, record)

    return record
}

func PrintSummary() {
    mu.Lock()
    defer mu.Unlock()

    if len(records) == 0 {
        fmt.Println("[AI Monitor] No usage records captured")
        return
    }

    fmt.Println("\n================ AI Usage Summary ================")

    var total float64

    for _, r := range records {
        total += r.EstimatedUSD

        fmt.Printf(
            "\n[%s]\nModel: %s\nInput Tokens: %d\nOutput Tokens: %d\nEstimated Cost: $%.6f\nLatency: %dms\n",
            r.Agent,
            r.Model,
            r.InputTokens,
            r.OutputTokens,
            r.EstimatedUSD,
            r.DurationMs,
        )
    }

    fmt.Printf("\nTotal Estimated Cost: $%.6f\n", total)
    fmt.Println("==================================================")
}
