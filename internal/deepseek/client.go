package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type thinkingParam struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model           string         `json:"model"`
	Messages        []Message      `json:"messages"`
	Stream          bool           `json:"stream"`
	Thinking        *thinkingParam `json:"thinking,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
}

type Usage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	ReasoningTokens       int `json:"reasoning_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens          int `json:"prompt_tokens"`
		CompletionTokens      int `json:"completion_tokens"`
		TotalTokens           int `json:"total_tokens"`
		PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
		CompletionTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type DebugInfo struct {
	URL        string          `json:"url"`
	Request    json.RawMessage `json:"request"`
	Response   json.RawMessage `json:"response,omitempty"`
	StatusCode int             `json:"status_code"`
	DurationMs int64           `json:"duration_ms"`
}

type ChatResult struct {
	Reply            string
	ReasoningContent string
	Usage            Usage
	FinishReason     string
	Debug            DebugInfo
}

type ModelStats struct {
	DurationMs      int64   `json:"duration_ms"`
	PromptTokens    int     `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	ReasoningTokens int     `json:"reasoning_tokens"`
	TotalTokens     int     `json:"total_tokens"`
	CacheHitTokens  int     `json:"cache_hit_tokens"`
	CacheMissTokens int     `json:"cache_miss_tokens"`
	TokensPerSec    float64 `json:"tokens_per_sec"`
	CostUSD         float64 `json:"cost_usd"`
}

type ModelCompareResult struct {
	ID               ModelTier  `json:"id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Model            string     `json:"model"`
	Thinking         string     `json:"thinking"`
	Reply            string     `json:"reply,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	FinishReason     string     `json:"finish_reason,omitempty"`
	Stats            ModelStats `json:"stats"`
	Error            string     `json:"error,omitempty"`
}

func NewClient(apiKey, model, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

func (c *Client) Chat(ctx context.Context, messages []Message) (ChatResult, error) {
	return c.ChatModel(ctx, messages, ModelSpec{
		Model:    c.model,
		Thinking: "disabled",
	})
}

func (c *Client) ChatModel(ctx context.Context, messages []Message, spec ModelSpec) (ChatResult, error) {
	start := time.Now()

	payload := chatRequest{
		Model:    spec.Model,
		Messages: messages,
		Stream:   false,
		Thinking: &thinkingParam{Type: spec.Thinking},
	}
	if spec.Thinking == "enabled" && spec.Effort != "" {
		payload.ReasoningEffort = spec.Effort
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResult{}, fmt.Errorf("marshal request: %w", err)
	}

	debug := DebugInfo{
		URL:     c.baseURL,
		Request: json.RawMessage(body),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		debug.DurationMs = time.Since(start).Milliseconds()
		return ChatResult{Debug: debug}, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	debug.StatusCode = resp.StatusCode
	debug.DurationMs = time.Since(start).Milliseconds()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResult{Debug: debug}, fmt.Errorf("read response: %w", err)
	}
	debug.Response = json.RawMessage(respBody)

	var result chatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return ChatResult{Debug: debug}, fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return ChatResult{Debug: debug}, fmt.Errorf("deepseek api error: %s", result.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return ChatResult{Debug: debug}, fmt.Errorf("deepseek api returned status %d: %s", resp.StatusCode, string(respBody))
	}
	if len(result.Choices) == 0 {
		return ChatResult{Debug: debug}, fmt.Errorf("empty response from deepseek")
	}

	usage := Usage{}
	if result.Usage != nil {
		usage.PromptTokens = result.Usage.PromptTokens
		usage.CompletionTokens = result.Usage.CompletionTokens
		usage.TotalTokens = result.Usage.TotalTokens
		usage.PromptCacheHitTokens = result.Usage.PromptCacheHitTokens
		usage.PromptCacheMissTokens = result.Usage.PromptCacheMissTokens
		if result.Usage.CompletionTokensDetails != nil {
			usage.ReasoningTokens = result.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}

	return ChatResult{
		Reply:            result.Choices[0].Message.Content,
		ReasoningContent: result.Choices[0].Message.ReasoningContent,
		Usage:            usage,
		FinishReason:     result.Choices[0].FinishReason,
		Debug:            debug,
	}, nil
}

func buildStats(spec ModelSpec, result ChatResult) ModelStats {
	durationMs := result.Debug.DurationMs
	tps := 0.0
	if durationMs > 0 && result.Usage.CompletionTokens > 0 {
		tps = float64(result.Usage.CompletionTokens) / (float64(durationMs) / 1000.0)
	}
	return ModelStats{
		DurationMs:       durationMs,
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		ReasoningTokens:  result.Usage.ReasoningTokens,
		TotalTokens:      result.Usage.TotalTokens,
		CacheHitTokens:   result.Usage.PromptCacheHitTokens,
		CacheMissTokens:  result.Usage.PromptCacheMissTokens,
		TokensPerSec:     tps,
		CostUSD:          EstimateCostUSD(spec.Model, result.Usage),
	}
}

type CompareProgress func(result ModelCompareResult)

// Compare запускает три уровня параллельно и отдаёт результаты по готовности.
func (c *Client) Compare(ctx context.Context, question string, onDone CompareProgress) []ModelCompareResult {
	specs := TierSpecs()
	results := make([]ModelCompareResult, len(specs))
	var wg sync.WaitGroup

	messages := []Message{{Role: "user", Content: question}}

	for i, spec := range specs {
		wg.Add(1)
		go func(i int, spec ModelSpec) {
			defer wg.Done()
			out := ModelCompareResult{
				ID:          spec.ID,
				Title:       spec.Title,
				Description: spec.Description,
				Model:       spec.Model,
				Thinking:    spec.Thinking,
			}

			res, err := c.ChatModel(ctx, messages, spec)
			if err != nil {
				out.Error = err.Error()
				out.Stats = ModelStats{DurationMs: res.Debug.DurationMs}
			} else {
				out.Reply = res.Reply
				out.ReasoningContent = res.ReasoningContent
				out.FinishReason = res.FinishReason
				out.Stats = buildStats(spec, res)
			}

			results[i] = out
			if onDone != nil {
				onDone(out)
			}
		}(i, spec)
	}

	wg.Wait()
	return results
}
