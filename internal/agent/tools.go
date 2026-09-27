package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcp"
)

const maxToolRounds = 12

const mcpToolHint = `ИНСТРУМЕНТЫ: рецепт ищи через web_search запросом на английском, затем web_fetch только по английской ссылке из выдачи. Сразу переведи выдержку через translate_en_ru. Ответ пользователю только по-русски: без иероглифов и без английских абзацев. Картинку блюда бери через web_image (q на английском) и вставь image_url отдельной строкой сразу под этим блюдом, без HTML. Погоду не вызывай, если о ней не спросили. Не отвечай, пока страница не открыта и не переведена. Граммы и время не выдумывай. Блок-схему не вызывай: её добавит система.`

// ToolEvent — один вызов MCP за ход диалога.
type ToolEvent struct {
	Name      string          `json:"name"`
	Arguments string          `json:"arguments,omitempty"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
	Response  json.RawMessage `json:"response,omitempty"`
}

// ToolExchange — ответ инструмента и сырой запрос/ответ MCP.
type ToolExchange struct {
	Text     string
	IsError  bool
	Request  json.RawMessage
	Response json.RawMessage
}

// TurnStep — один шаг хода: запрос к модели или вызов MCP.
type TurnStep struct {
	Kind       string          `json:"kind"`
	Title      string          `json:"title"`
	URL        string          `json:"url,omitempty"`
	Status     int             `json:"status,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
	Request    json.RawMessage `json:"request,omitempty"`
	Response   json.RawMessage `json:"response,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// ToolSource — внешние инструменты, которые модель может вызвать.
type ToolSource interface {
	Tools() []deepseek.Tool
	Call(ctx context.Context, name, arguments string) (ToolExchange, error)
}

// MCPTools адаптирует stdio-клиент MCP к агенту.
type MCPTools struct {
	Client *mcp.Client
	Server string
}

func (m MCPTools) Tools() []deepseek.Tool {
	if m.Client == nil {
		return nil
	}
	src := m.Client.Tools()
	out := make([]deepseek.Tool, 0, len(src))
	for _, tool := range src {
		params := tool.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		desc := tool.Description
		if m.Server != "" && tool.Name != "draw_flow" {
			desc = "Сервер «" + m.Server + "». " + desc
		}
		out = append(out, deepseek.Tool{
			Name:        tool.Name,
			Description: desc,
			Parameters:  params,
		})
	}
	return out
}

func (m MCPTools) Call(ctx context.Context, name, arguments string) (ToolExchange, error) {
	if m.Client == nil {
		return ToolExchange{IsError: true}, fmt.Errorf("mcp client is nil")
	}
	raw := strings.TrimSpace(arguments)
	if raw == "" {
		raw = "{}"
	}
	if !json.Valid([]byte(raw)) {
		return ToolExchange{IsError: true}, fmt.Errorf("arguments are not json")
	}
	res, err := m.Client.Call(ctx, name, json.RawMessage(raw))
	ex := ToolExchange{
		Text:     res.Text,
		IsError:  res.IsError,
		Request:  res.Request,
		Response: res.Response,
	}
	if err != nil {
		return ex, err
	}
	return ex, nil
}

// ToolSet склеивает несколько источников. Call идёт в тот, где есть имя.
type ToolSet []ToolSource

func (s ToolSet) Tools() []deepseek.Tool {
	var out []deepseek.Tool
	for _, src := range s {
		if src == nil {
			continue
		}
		out = append(out, src.Tools()...)
	}
	return out
}

func (s ToolSet) Call(ctx context.Context, name, arguments string) (ToolExchange, error) {
	for _, src := range s {
		if src == nil {
			continue
		}
		for _, tool := range src.Tools() {
			if tool.Name == name {
				return src.Call(ctx, name, arguments)
			}
		}
	}
	return ToolExchange{IsError: true}, fmt.Errorf("unknown tool %q", name)
}

// WithTools подключает MCP. Клоны агента инструменты не наследуют.
func (a *Agent) WithTools(src ToolSource) *Agent {
	a.tools = src
	return a
}

// Tools возвращает подключённый источник (может быть nil).
func (a *Agent) Tools() ToolSource {
	return a.tools
}

type toolLLM interface {
	ChatTools(ctx context.Context, messages []deepseek.Message, tools []deepseek.Tool) (deepseek.ChatResult, error)
}

func (a *Agent) completeWithTools(ctx context.Context, llm LLM, messages []deepseek.Message) (deepseek.ChatResult, []ToolEvent, []TurnStep, error) {
	tools := a.toolDefs()
	caller, ok := llm.(toolLLM)
	if !ok || len(tools) == 0 {
		chat, err := llm.Chat(ctx, messages)
		return chat, nil, []TurnStep{llmStep(1, chat, err)}, err
	}

	prompt := append([]deepseek.Message(nil), messages...)
	prompt = append(prompt, deepseek.Message{Role: "system", Content: mcpToolHint})

	var events []ToolEvent
	var steps []TurnStep
	var last deepseek.ChatResult
	apiN := 0
	foreignNudged := false
	for round := 0; round < maxToolRounds; round++ {
		chat, err := caller.ChatTools(ctx, prompt, tools)
		apiN++
		steps = append(steps, llmStep(apiN, chat, err))
		if err != nil && len(events) == 0 && chat.Debug.StatusCode == 400 {
			plain, plainErr := llm.Chat(ctx, messages)
			apiN++
			steps = append(steps, llmStep(apiN, plain, plainErr))
			return plain, nil, steps, plainErr
		}
		if err != nil {
			return chat, events, steps, err
		}
		last = chat
		if len(chat.ToolCalls) == 0 {
			if round < maxToolRounds-1 {
				if mediaChainOpen(events) {
					prompt = append(prompt,
						deepseek.Message{Role: "assistant", Content: chat.Reply},
						deepseek.Message{Role: "user", Content: "Цепочка не закончена. Сразу вызови следующий инструмент: после search_media — summarize_media с inventory_path, после summarize_media — chart_media с summary_path. Текст не пиши, пока нет графика."},
					)
					continue
				}
				if researchNeedsFetch(events) {
					prompt = append(prompt,
						deepseek.Message{Role: "assistant", Content: chat.Reply},
						deepseek.Message{Role: "user", Content: "Поиск уже есть, страницу ещё не открывал. Сразу вызови web_fetch по одной английской ссылке из результатов. Ответ пользователю только после перевода."},
					)
					continue
				}
				if needsTranslate(events) {
					prompt = append(prompt,
						deepseek.Message{Role: "assistant", Content: chat.Reply},
						deepseek.Message{Role: "user", Content: "Страница открыта, перевода ещё нет. Сразу вызови translate_en_ru и передай короткую английскую выдержку с граммами и шагами. Текст пользователю пока не пиши."},
					)
					continue
				}
				if recipeNeedsPhoto(events) {
					prompt = append(prompt,
						deepseek.Message{Role: "assistant", Content: chat.Reply},
						deepseek.Message{Role: "user", Content: "Страница рецепта уже открыта. Сразу вызови web_image с английским названием блюда. Текст пользователю пока не пиши."},
					)
					continue
				}
				if !foreignNudged && hasCJK(chat.Reply) {
					foreignNudged = true
					prompt = append(prompt,
						deepseek.Message{Role: "assistant", Content: chat.Reply},
						deepseek.Message{Role: "user", Content: "В ответе есть иероглифы. Перепиши целиком по-русски, без китайского и без английских фраз. Фото блюд оставь на своих местах."},
					)
					continue
				}
			}
			chat, events, steps = a.sealTurn(ctx, chat, events, steps)
			return chat, events, steps, nil
		}
		prompt = append(prompt, deepseek.Message{
			Role:      "assistant",
			Content:   chat.Reply,
			ToolCalls: chat.ToolCalls,
		})
		for _, call := range chat.ToolCalls {
			ev := a.execTool(ctx, call)
			events = append(events, ev)
			steps = append(steps, mcpStep(ev))
			prompt = append(prompt, deepseek.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    ev.Result,
			})
		}
	}
	if strings.TrimSpace(last.Reply) != "" && len(last.ToolCalls) == 0 {
		return last, events, steps, nil
	}
	final, err := caller.ChatTools(ctx, prompt, nil)
	apiN++
	steps = append(steps, llmStep(apiN, final, err))
	final, events, steps = a.sealTurn(ctx, final, events, steps)
	return final, events, steps, err
}

func (a *Agent) sealTurn(ctx context.Context, chat deepseek.ChatResult, events []ToolEvent, steps []TurnStep) (deepseek.ChatResult, []ToolEvent, []TurnStep) {
	events, steps = a.ensureFlow(ctx, events, steps)
	chat.Reply = attachChartURL(chat.Reply, events)
	return chat, events, steps
}

func (a *Agent) ensureFlow(ctx context.Context, events []ToolEvent, steps []TurnStep) ([]ToolEvent, []TurnStep) {
	if a.tools == nil || !needsFlow(events) {
		return events, steps
	}
	raw, err := json.Marshal(map[string]any{
		"title": "Ход запроса",
		"steps": flowSteps(events),
	})
	if err != nil {
		return events, steps
	}
	ex, err := a.tools.Call(ctx, "draw_flow", string(raw))
	ev := ToolEvent{Name: "draw_flow", Arguments: string(raw), Result: ex.Text, IsError: ex.IsError || err != nil, Request: ex.Request, Response: ex.Response}
	if err != nil && ev.Result == "" {
		ev.Result = err.Error()
	}
	events = append(events, ev)
	steps = append(steps, mcpStep(ev))
	return events, steps
}

func needsFlow(events []ToolEvent) bool {
	for _, ev := range events {
		if ev.Name != "" && ev.Name != "draw_flow" {
			return true
		}
	}
	return false
}

func needsTranslate(events []ToolEvent) bool {
	fetched, translated, tries := false, false, 0
	for _, ev := range events {
		if ev.Name == "translate_en_ru" {
			tries++
			if !ev.IsError {
				translated = true
			}
		}
		if ev.IsError {
			continue
		}
		if ev.Name == "web_fetch" {
			fetched = true
		}
	}
	return fetched && !translated && tries < 2
}

func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x3040 && r <= 0x30FF || r >= 0x3400 && r <= 0x9FFF || r >= 0xAC00 && r <= 0xD7AF {
			return true
		}
	}
	return false
}

func recipeNeedsPhoto(events []ToolEvent) bool {
	fetched, tries, shown := false, 0, false
	for _, ev := range events {
		if ev.Name == "web_image" {
			tries++
			if !ev.IsError {
				shown = true
			}
		}
		if ev.IsError {
			continue
		}
		if ev.Name == "web_fetch" {
			fetched = true
		}
	}
	return fetched && !shown && tries < 2
}

func researchNeedsFetch(events []ToolEvent) bool {
	search, fetch := false, false
	for _, ev := range events {
		if ev.IsError {
			continue
		}
		switch ev.Name {
		case "web_search":
			search = true
		case "web_fetch":
			fetch = true
		case "web_image":
			fetch = true
		}
	}
	return search && !fetch
}

func flowSteps(events []ToolEvent) []map[string]string {
	var out []map[string]string
	for _, ev := range events {
		if ev.Name == "" || ev.Name == "draw_flow" {
			continue
		}
		out = append(out, map[string]string{
			"server": serverOf(ev.Name),
			"tool":   ev.Name,
			"detail": stepDetail(ev.Arguments),
		})
	}
	return out
}

func serverOf(tool string) string {
	switch tool {
	case "web_search", "web_fetch", "web_image":
		return "поиск"
	case "translate_en_ru":
		return "перевод"
	case "get_weather", "observe_city":
		return "погода"
	case "search_media", "summarize_media", "chart_media":
		return "медиа"
	default:
		return "mcp"
	}
}

func stepDetail(args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return trimDetail(args)
	}
	for _, key := range []string{"q", "query", "url", "city", "text"} {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			return trimDetail(s)
		}
	}
	return trimDetail(args)
}

func trimDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 90 {
		return s[:90] + "…"
	}
	return s
}

func mediaChainOpen(events []ToolEvent) bool {
	searched := false
	charted := false
	for _, ev := range events {
		if ev.IsError {
			continue
		}
		switch ev.Name {
		case "search_media", "summarize_media":
			searched = true
		case "chart_media":
			charted = true
		}
	}
	return searched && !charted
}

func attachChartURL(reply string, events []ToolEvent) string {
	for _, ev := range events {
		var payload struct {
			ImageURL string `json:"image_url"`
		}
		if json.Unmarshal([]byte(ev.Result), &payload) != nil || payload.ImageURL == "" {
			continue
		}
		if strings.Contains(reply, payload.ImageURL) {
			return reply
		}
		if strings.TrimSpace(reply) == "" {
			return payload.ImageURL
		}
		return strings.TrimRight(reply, "\n") + "\n" + payload.ImageURL
	}
	return reply
}

func llmStep(n int, chat deepseek.ChatResult, err error) TurnStep {
	step := TurnStep{
		Kind:       "llm",
		Title:      fmt.Sprintf("API · запрос %d", n),
		URL:        chat.Debug.URL,
		Status:     chat.Debug.StatusCode,
		DurationMs: chat.Debug.DurationMs,
		Request:    chat.Debug.Request,
		Response:   chat.Debug.Response,
	}
	if err != nil {
		step.Error = err.Error()
	}
	return step
}

func mcpStep(ev ToolEvent) TurnStep {
	step := TurnStep{
		Kind:     "mcp",
		Title:    "MCP · " + ev.Name,
		Request:  ev.Request,
		Response: ev.Response,
	}
	if ev.IsError && ev.Result != "" {
		step.Error = ev.Result
	}
	if len(step.Request) == 0 && strings.TrimSpace(ev.Arguments) != "" {
		if json.Valid([]byte(ev.Arguments)) {
			step.Request = json.RawMessage(ev.Arguments)
		}
	}
	if len(step.Response) == 0 {
		raw, err := json.Marshal(map[string]any{"text": ev.Result, "is_error": ev.IsError})
		if err == nil {
			step.Response = raw
		}
	}
	return step
}

func (a *Agent) toolDefs() []deepseek.Tool {
	if a.tools == nil {
		return nil
	}
	all := a.tools.Tools()
	out := make([]deepseek.Tool, 0, len(all))
	for _, tool := range all {
		if tool.Name == "draw_flow" {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func (a *Agent) execTool(ctx context.Context, call deepseek.ToolCall) ToolEvent {
	ev := ToolEvent{Name: call.Name, Arguments: call.Arguments}
	if a.tools == nil {
		ev.IsError = true
		ev.Result = "Инструменты не подключены."
		return ev
	}
	ex, err := a.tools.Call(ctx, call.Name, call.Arguments)
	ev.Request = ex.Request
	ev.Response = ex.Response
	if err != nil {
		ev.IsError = true
		if ex.Text != "" {
			ev.Result = ex.Text
		} else {
			ev.Result = err.Error()
		}
		return ev
	}
	ev.IsError = ex.IsError
	ev.Result = ex.Text
	return ev
}
