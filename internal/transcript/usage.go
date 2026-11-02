package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"

	"github.com/emdzej/acc/internal/model"
)

// Usage computes token/cost usage for a Claude Code transcript.
//
// Claude Code records a running "cost-state" entry (cost, per-model tokens,
// durations, lines changed); the latest one wins. Without it (older versions,
// or before the first one is written) tokens are summed from assistant
// messages, deduplicated by message id because streamed messages repeat usage.
func Usage(path string) (*model.Usage, error) {
	real, err := checkPath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type msgUsage struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	}
	var (
		summed   model.Usage
		seen     = map[string]bool{}
		models   = map[string]bool{}
		costLine []byte
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256<<10), 16<<20)
	for sc.Scan() {
		b := sc.Bytes()
		var l struct {
			Type    string `json:"type"`
			Message struct {
				ID    string    `json:"id"`
				Model string    `json:"model"`
				Usage *msgUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(b, &l) != nil {
			continue
		}
		switch l.Type {
		case "cost-state":
			costLine = append(costLine[:0], b...)
		case "assistant":
			u := l.Message.Usage
			if u == nil || (l.Message.ID != "" && seen[l.Message.ID]) {
				continue
			}
			seen[l.Message.ID] = true
			if l.Message.Model != "" && l.Message.Model != "<synthetic>" {
				models[l.Message.Model] = true
			}
			summed.InputTokens += u.InputTokens
			summed.OutputTokens += u.OutputTokens
			summed.CacheReadTokens += u.CacheReadInputTokens
			summed.CacheWriteTokens += u.CacheCreationInputTokens
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	if costLine != nil {
		var cs struct {
			TotalCostUSD     float64 `json:"totalCostUSD"`
			TotalAPIDuration int64   `json:"totalAPIDuration"`
			TotalDuration    int64   `json:"totalDuration"`
			LinesAdded       int64   `json:"totalLinesAdded"`
			LinesRemoved     int64   `json:"totalLinesRemoved"`
			UnknownCost      bool    `json:"hasUnknownModelCost"`
			ModelUsage       map[string]struct {
				InputTokens              int64   `json:"inputTokens"`
				OutputTokens             int64   `json:"outputTokens"`
				CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
				CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
				CostUSD                  float64 `json:"costUSD"`
			} `json:"modelUsage"`
		}
		if json.Unmarshal(costLine, &cs) == nil {
			u := &model.Usage{
				CostUSD: cs.TotalCostUSD, CostKnown: !cs.UnknownCost,
				APIDurationMs: cs.TotalAPIDuration, DurationMs: cs.TotalDuration,
				LinesAdded: cs.LinesAdded, LinesRemoved: cs.LinesRemoved,
			}
			for name, mu := range cs.ModelUsage {
				u.InputTokens += mu.InputTokens
				u.OutputTokens += mu.OutputTokens
				u.CacheReadTokens += mu.CacheReadInputTokens
				u.CacheWriteTokens += mu.CacheCreationInputTokens
				u.Models = append(u.Models, name)
			}
			sort.Strings(u.Models)
			return u, nil
		}
	}
	for name := range models {
		summed.Models = append(summed.Models, name)
	}
	sort.Strings(summed.Models)
	return &summed, nil
}
