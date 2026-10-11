package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const probeShotsDefault = 3

var numCtxRE = regexp.MustCompile(`(?m)^\s*num_ctx\s+(\d+)\s*$`)

// ProbeShot — один короткий запрос в проверке стабильности.
type ProbeShot struct {
	N          int    `json:"n"`
	OK         bool   `json:"ok"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Reply      string `json:"reply,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ProbeReport — доступ по сети, несколько запросов и базовые лимиты локальной модели.
type ProbeReport struct {
	Host            string            `json:"host"`
	Model           string            `json:"model"`
	NetworkOK       bool              `json:"network_ok"`
	NetworkMs       int64             `json:"network_ms"`
	ModelIDs        []string          `json:"model_ids,omitempty"`
	ContextLength   int               `json:"context_length"`
	NumCtx          int               `json:"num_ctx"`
	Quant           string            `json:"quant,omitempty"`
	ParameterSize   string            `json:"parameter_size,omitempty"`
	RateLimit       string            `json:"rate_limit"`
	RateHeaders     map[string]string `json:"rate_headers,omitempty"`
	Shots           []ProbeShot       `json:"shots"`
	Stable          bool              `json:"stable"`
	AppContextLimit int               `json:"app_context_limit,omitempty"`
	Summary         string            `json:"summary"`
}

type probeChatBody struct {
	Model           string    `json:"model"`
	Messages        []Message `json:"messages"`
	MaxTokens       int       `json:"max_tokens"`
	Temperature     float64   `json:"temperature"`
	ReasoningEffort string    `json:"reasoning_effort"`
}

// Probe проверяет локальный HTTP API: список моделей, окно контекста и несколько коротких ответов.
func (c *Client) Probe(ctx context.Context, shots int) (ProbeReport, error) {
	if shots <= 0 {
		shots = probeShotsDefault
	}
	if shots > 5 {
		shots = 5
	}
	origin, err := c.origin()
	report := ProbeReport{Host: origin, Model: c.model}
	if err != nil {
		report.RateLimit = "хост не разобран"
		report.Summary = FormatProbe(report)
		return report, err
	}

	netCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	ids, status, ms, netErr := c.getModelIDs(netCtx, origin)
	cancel()
	report.NetworkMs = ms
	report.ModelIDs = ids
	report.NetworkOK = netErr == nil && status >= 200 && status < 300 && len(ids) > 0
	if !report.NetworkOK {
		if netErr != nil {
			report.RateLimit = "до модели по сети не достучаться"
			report.Summary = FormatProbe(report)
			return report, netErr
		}
		report.RateLimit = fmt.Sprintf("список моделей ответил %d", status)
		report.Summary = FormatProbe(report)
		return report, fmt.Errorf("models status %d", status)
	}

	showCtx, showCancel := context.WithTimeout(ctx, 8*time.Second)
	c.fillShow(showCtx, origin, &report)
	showCancel()

	headers := map[string]string{}
	for i := 1; i <= shots; i++ {
		shotCtx, shotCancel := context.WithTimeout(ctx, 45*time.Second)
		shot, rates := c.oneShot(shotCtx, i)
		shotCancel()
		report.Shots = append(report.Shots, shot)
		mergeRateHeaders(headers, rates)
	}
	if len(headers) > 0 {
		report.RateHeaders = headers
	}
	report.Stable = len(report.Shots) == shots && allShotsOK(report.Shots)
	report.RateLimit = rateNote(headers, report.Shots)
	report.Summary = FormatProbe(report)
	if !report.Stable {
		return report, fmt.Errorf("не все запросы прошли")
	}
	return report, nil
}

func (c *Client) oneShot(ctx context.Context, n int) (ProbeShot, map[string]string) {
	start := time.Now()
	shot := ProbeShot{N: n}
	body, err := json.Marshal(probeChatBody{
		Model:           c.model,
		Messages:        []Message{{Role: "user", Content: "Ответь одним словом: ок"}},
		MaxTokens:       16,
		Temperature:     0,
		ReasoningEffort: "none",
	})
	if err != nil {
		shot.Error = err.Error()
		shot.DurationMs = time.Since(start).Milliseconds()
		return shot, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		shot.Error = err.Error()
		shot.DurationMs = time.Since(start).Milliseconds()
		return shot, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	shot.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		shot.Error = wrapNet(err).Error()
		return shot, nil
	}
	defer resp.Body.Close()
	shot.Status = resp.StatusCode
	rates := rateHeaders(resp.Header)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		shot.Error = err.Error()
		return shot, rates
	}
	if resp.StatusCode >= 400 {
		shot.Error = strings.TrimSpace(string(raw))
		return shot, rates
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		shot.Error = err.Error()
		return shot, rates
	}
	if parsed.Error != nil {
		shot.Error = parsed.Error.Message
		return shot, rates
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		shot.Error = "пустой ответ"
		return shot, rates
	}
	shot.Reply = strings.TrimSpace(parsed.Choices[0].Message.Content)
	shot.OK = true
	return shot, rates
}

func (c *Client) getModelIDs(ctx context.Context, origin string) ([]string, int, int64, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/v1/models", nil)
	if err != nil {
		return nil, 0, time.Since(start).Milliseconds(), err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return nil, 0, ms, wrapNet(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, ms, err
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, resp.StatusCode, ms, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) != "" {
			ids = append(ids, item.ID)
		}
	}
	return ids, resp.StatusCode, ms, nil
}

func (c *Client) fillShow(ctx context.Context, origin string, report *ProbeReport) {
	body, _ := json.Marshal(map[string]string{"name": c.model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/show", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return
	}
	var payload struct {
		Parameters string `json:"parameters"`
		Details    struct {
			QuantizationLevel string `json:"quantization_level"`
			ParameterSize     string `json:"parameter_size"`
		} `json:"details"`
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	report.ContextLength = contextLength(payload.ModelInfo)
	report.NumCtx = numCtx(payload.Parameters)
	report.Quant = payload.Details.QuantizationLevel
	report.ParameterSize = payload.Details.ParameterSize
}

func (c *Client) origin() (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("адрес локальной модели не разобран")
	}
	return u.Scheme + "://" + u.Host, nil
}

func contextLength(info map[string]any) int {
	for key, value := range info {
		if !strings.HasSuffix(key, "context_length") {
			continue
		}
		switch n := value.(type) {
		case float64:
			return int(n)
		case json.Number:
			v, _ := n.Int64()
			return int(v)
		}
	}
	return 0
}

func numCtx(parameters string) int {
	m := numCtxRE.FindStringSubmatch(parameters)
	if len(m) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func rateHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for key, values := range h {
		low := strings.ToLower(key)
		if low == "retry-after" || strings.Contains(low, "ratelimit") || strings.Contains(low, "rate-limit") {
			out[low] = strings.Join(values, ", ")
		}
	}
	return out
}

func mergeRateHeaders(dst map[string]string, src map[string]string) {
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
}

func allShotsOK(shots []ProbeShot) bool {
	if len(shots) == 0 {
		return false
	}
	for _, shot := range shots {
		if !shot.OK {
			return false
		}
	}
	return true
}

func rateNote(headers map[string]string, shots []ProbeShot) string {
	rejected := 0
	for _, shot := range shots {
		if shot.Status == http.StatusTooManyRequests {
			rejected++
		}
	}
	if rejected > 0 {
		return fmt.Sprintf("сервер ограничил частоту: %d из %d запросов получили 429", rejected, len(shots))
	}
	if len(headers) > 0 {
		parts := make([]string, 0, len(headers))
		for k, v := range headers {
			parts = append(parts, k+"="+v)
		}
		return "сервер прислал лимит запросов: " + strings.Join(parts, ", ")
	}
	return "отдельного лимита запросов нет: заголовков Retry-After и RateLimit нет, ответов 429 не было"
}

// FormatProbe собирает короткий отчёт для карточки в чате.
func FormatProbe(r ProbeReport) string {
	var b strings.Builder
	if r.NetworkOK {
		fmt.Fprintf(&b, "Сеть: модель доступна на %s за %d мс (%s).", r.Host, r.NetworkMs, strings.Join(r.ModelIDs, ", "))
	} else if r.Host != "" {
		fmt.Fprintf(&b, "Сеть: %s не ответил.", r.Host)
	} else {
		b.WriteString("Сеть: адрес модели не задан.")
	}
	if r.ContextLength > 0 {
		fmt.Fprintf(&b, " Окно файла — %d токенов.", r.ContextLength)
	}
	if r.NumCtx > 0 {
		fmt.Fprintf(&b, " В Modelfile для генерации num_ctx = %d.", r.NumCtx)
	}
	if r.AppContextLimit > 0 {
		fmt.Fprintf(&b, " Счётчик чата режет запрос на %d.", r.AppContextLimit)
	}
	if len(r.Shots) > 0 {
		ok := 0
		var ms int64
		for _, shot := range r.Shots {
			if shot.OK {
				ok++
			}
			ms += shot.DurationMs
		}
		fmt.Fprintf(&b, " Стабильность: %d из %d запросов с текстом ответа, суммарно %d мс.", ok, len(r.Shots), ms)
	}
	if r.RateLimit != "" {
		b.WriteString(" Rate limit: " + r.RateLimit + ".")
	}
	return b.String()
}
