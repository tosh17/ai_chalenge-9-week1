package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcp"
	"github.com/tosh17/deepseek-service/internal/observe"
)

const observeGap = 2 * time.Minute

// ObservationTools читает архив. Record=true добавляет запись замера; это только процесс сбора.
type ObservationTools struct {
	Store  *observe.Store
	MCP    *mcp.Client
	Record bool
}

func (o ObservationTools) Tools() []deepseek.Tool {
	if o.Store == nil {
		return nil
	}
	var out []deepseek.Tool
	if o.Record {
		out = append(out, deepseek.Tool{
			Name:        "record_observation",
			Description: "Снять текущие наблюдения города (температура, погода, восход, закат, фаза луны) и дописать их в архив JSONL.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string","description":"Город, например Волгоград"}},"required":["city"]}`),
		})
	}
	out = append(out, deepseek.Tool{
		Name:        "observation_report",
		Description: "Подробный ряд замеров: время, температура, погода, влажность, ветер, восход, закат, фаза луны. Без from и to возвращает весь архив и фактическое время первого и последнего замера. Период передавай сам, если пользователь его назвал. Выводов нет.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string","description":"Город"},"from":{"type":"string","description":"Начало периода, если пользователь его назвал: местное 2006-01-02T15:04 или RFC3339"},"to":{"type":"string","description":"Конец периода, если пользователь его назвал: местное 2006-01-02T15:04 или RFC3339"},"hours":{"type":"integer","description":"Только если пользователь просил последние N часов"}},"required":["city"]}`),
	})
	return out
}

func (o ObservationTools) Call(ctx context.Context, name, arguments string) (ToolExchange, error) {
	switch name {
	case "record_observation":
		if !o.Record {
			return ToolExchange{Text: "запись замеров делает отдельный процесс сбора", IsError: true}, nil
		}
		return o.record(ctx, arguments)
	case "observation_report":
		return o.report(arguments)
	default:
		return ToolExchange{IsError: true}, fmt.Errorf("unknown observation tool %q", name)
	}
}

func (o ObservationTools) record(ctx context.Context, arguments string) (ToolExchange, error) {
	var args struct {
		City string `json:"city"`
	}
	_ = json.Unmarshal([]byte(arguments), &args)
	sample, fresh, err := o.RecordCity(ctx, args.City)
	if err != nil {
		return ToolExchange{Text: err.Error(), IsError: true}, nil
	}
	note := "записано"
	if !fresh {
		note = "свежая запись уже есть, новую не дублировал"
	}
	text := fmt.Sprintf("%s: %s, %.1f °C, %s, солнце %s–%s, луна %s (%.2f). Файл: %s",
		note, sample.City, sample.TemperatureC, sample.Weather, dash(sample.Sunrise), dash(sample.Sunset), dash(sample.MoonName), sample.MoonPhase, o.Store.Dir())
	return ToolExchange{Text: text}, nil
}

// RecordCity запрашивает observe_city и дописывает файл. Вызывают инструмент и планировщик.
func (o ObservationTools) RecordCity(ctx context.Context, city string) (observe.Sample, bool, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return observe.Sample{}, false, fmt.Errorf("нужен город")
	}
	if o.MCP == nil || o.Store == nil {
		return observe.Sample{}, false, fmt.Errorf("архив наблюдений не подключён")
	}
	res, err := o.MCP.Call(ctx, "observe_city", json.RawMessage(fmt.Sprintf(`{"city":%q}`, city)))
	if err != nil {
		return observe.Sample{}, false, err
	}
	if res.IsError {
		return observe.Sample{}, false, fmt.Errorf("%s", res.Text)
	}
	var sample observe.Sample
	if err := json.Unmarshal([]byte(res.Text), &sample); err != nil {
		return observe.Sample{}, false, fmt.Errorf("observe_city вернул не JSON: %w", err)
	}
	if sample.City == "" {
		sample.City = city
	}
	return o.Store.Append(sample, observeGap)
}

func (o ObservationTools) report(arguments string) (ToolExchange, error) {
	var args struct {
		City  string `json:"city"`
		Hours int    `json:"hours"`
		From  string `json:"from"`
		To    string `json:"to"`
	}
	_ = json.Unmarshal([]byte(arguments), &args)
	if strings.TrimSpace(args.City) == "" {
		return ToolExchange{Text: "нужен город", IsError: true}, nil
	}
	from, to := time.Time{}, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if t, ok := parseBound(args.From); ok {
		from = t
	}
	if t, ok := parseBound(args.To); ok {
		to = t
	}
	if !boundSet(args.From) && !boundSet(args.To) && args.Hours > 0 {
		from = time.Now().Add(-time.Duration(args.Hours) * time.Hour)
		to = time.Now().Add(time.Minute)
	}
	rows, err := o.Store.Between(args.City, from, to)
	if err != nil {
		return ToolExchange{Text: err.Error(), IsError: true}, nil
	}
	return ToolExchange{Text: observe.ReportText(args.City, rows)}, nil
}

func boundSet(value string) bool {
	_, ok := parseBound(value)
	return ok
}

func parseBound(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func dash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

// RunQuiet вызывает модель с инструментами и не пишет реплику в память чата.
func (a *Agent) RunQuiet(ctx context.Context, providerID, userMsg string) (Result, error) {
	if providerID == "" {
		providerID = a.defaultID
	}
	b, ok := a.backends[providerID]
	if !ok {
		return Result{}, fmt.Errorf("agent %q: unknown provider %q", a.name, providerID)
	}
	messages := []deepseek.Message{
		{Role: "system", Content: "Ты сборщик наблюдений. Вызови ровно один указанный инструмент. Не выдумывай температуру, солнце и луну."},
		{Role: "user", Content: userMsg},
	}
	chat, events, steps, err := a.completeWithTools(ctx, b.llm, messages)
	return Result{
		Reply:      chat.Reply,
		Provider:   providerID,
		Model:      b.model,
		ToolEvents: events,
		Steps:      steps,
	}, err
}
