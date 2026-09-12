package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tosh17/deepseek-service/internal/memory"
	"github.com/tosh17/deepseek-service/internal/tokens"
)

// StrategyCompareSide — прогон одной стратегии на сценарии ТЗ.
type StrategyCompareSide struct {
	Kind         string                 `json:"kind"`
	Title        string                 `json:"title"`
	FinalReply   string                 `json:"final_reply"`
	Session      tokens.SessionTotals   `json:"session"`
	Facts        map[string]string      `json:"facts,omitempty"`
	Turns        int                    `json:"turns"`
	Strategy     *StrategyInfo          `json:"strategy,omitempty"`
	ProbeOK      bool                   `json:"probe_ok"`
	ProbeNotes   string                 `json:"probe_notes,omitempty"`
	Error        string                 `json:"error,omitempty"`
	DurationMs   int64                  `json:"duration_ms"`
}

// StrategyCompareResult — сравнение трёх стратегий.
type StrategyCompareResult struct {
	Scenario []string              `json:"scenario"`
	Probe    string                `json:"probe"`
	Sides    []StrategyCompareSide `json:"sides"`
	Report   string                `json:"report"`
}

var tzScenarioSteps = []string{
	"Мы собираем ТЗ на мобильное приложение доставки еды. Название проекта: FoodDash.",
	"Платформы: iOS и Android. Срок разработки — 3 месяца.",
	"Бюджет не больше 2 млн рублей. Команда: 1 iOS, 1 Android, 1 backend, 1 дизайн.",
	"Основной сценарий: заказ еды из ресторанов рядом с пользователем, оплата картой.",
	"Ограничение: без наличных, доставка только в радиусе 5 км.",
	"Предпочтение заказчика: тёмный UI, push-уведомления о статусе заказа.",
	"Решение: backend на Go, мобильные клиенты на Flutter (один код на обе платформы).",
	"Договорённость: MVP без подписок, только комиссия с ресторана 15%.",
	"Важное имя контактного лица — Анна, созвоны по вторникам в 11:00.",
	"Секретный код проекта для проверки памяти: ORANGE-42.",
	"Ещё деталь: минимальный заказ 600 рублей, чаевые курьеру опциональны.",
	"Зафиксируй, что аналитика — через Amplitude, а карты — Yandex Maps.",
}

var tzProbe = "Кратко списком: название проекта, платформы/стек, бюджет и срок, радиус доставки, комиссия, контакт (Анна), секретный код ORANGE-42, аналитика и карты. Если чего-то не помнишь — так и скажи."

func strategyTitle(kind string) string {
	switch kind {
	case StrategySliding:
		return "Sliding Window"
	case StrategyFacts:
		return "Sticky Facts"
	case StrategyBranch:
		return "Branching"
	default:
		return kind
	}
}

func probeScore(reply string) (bool, string) {
	low := strings.ToLower(reply)
	checks := []struct {
		ok   bool
		name string
	}{
		{strings.Contains(low, "fooddash") || strings.Contains(low, "food dash"), "название FoodDash"},
		{strings.Contains(low, "flutter") || (strings.Contains(low, "ios") && strings.Contains(low, "android")), "платформы/стек"},
		{strings.Contains(low, "2") && (strings.Contains(low, "млн") || strings.Contains(low, "миллион") || strings.Contains(low, "000")), "бюджет"},
		{strings.Contains(low, "3") && strings.Contains(low, "месяц"), "срок"},
		{strings.Contains(low, "5") && strings.Contains(low, "км"), "радиус 5 км"},
		{strings.Contains(low, "15"), "комиссия 15%"},
		{strings.Contains(low, "анна") || strings.Contains(low, "anna"), "контакт Анна"},
		{strings.Contains(low, "orange-42") || strings.Contains(low, "orange 42"), "код ORANGE-42"},
		{strings.Contains(low, "amplitude"), "Amplitude"},
		{strings.Contains(low, "yandex") || strings.Contains(low, "яндекс"), "Yandex Maps"},
	}
	var missing []string
	hit := 0
	for _, c := range checks {
		if c.ok {
			hit++
		} else {
			missing = append(missing, c.name)
		}
	}
	ok := hit >= 7
	note := fmt.Sprintf("найдено %d/%d ключевых деталей", hit, len(checks))
	if len(missing) > 0 {
		note += "; нет: " + strings.Join(missing, ", ")
	}
	return ok, note
}

// RunStrategyCompare прогоняет один сценарий ТЗ на sliding / facts / branch.
func (a *Agent) RunStrategyCompare(ctx context.Context, provider string) (StrategyCompareResult, error) {
	if provider == "" {
		provider = a.defaultID
	}
	dir, err := os.MkdirTemp("", "day10-strategy-*")
	if err != nil {
		return StrategyCompareResult{}, err
	}
	defer os.RemoveAll(dir)

	kinds := []string{StrategySliding, StrategyFacts, StrategyBranch}
	out := StrategyCompareResult{
		Scenario: append([]string{}, tzScenarioSteps...),
		Probe:    tzProbe,
	}

	for _, kind := range kinds {
		side := a.runOneStrategyScenario(ctx, provider, dir, kind)
		out.Sides = append(out.Sides, side)
	}
	out.Report = formatStrategyReport(out)
	return out, nil
}

func (a *Agent) runOneStrategyScenario(ctx context.Context, provider, dir, kind string) StrategyCompareSide {
	side := StrategyCompareSide{
		Kind:  kind,
		Title: strategyTitle(kind),
	}
	start := time.Now()
	store, err := memory.Open(filepath.Join(dir, kind+".json"))
	if err != nil {
		side.Error = err.Error()
		return side
	}
	demo := a.CloneWithMemory("cmp-"+kind, store)
	cfg := ContextStrategy{
		Kind:           kind,
		SlidingWindowN: 6, // намеренно узкое окно — чтобы sliding терял ранние факты
		FactsWindowN:   4,
		BranchWindowN:  0,
	}
	demo.WithStrategy(cfg)
	// compress выкл — работаем только стратегией
	demo.compression.Enabled = false

	steps := append([]string{}, tzScenarioSteps...)
	// Для branching: после половины — fork на A/B, продолжаем в A, в B кладём отвлекающий ход,
	// затем снова A и финальный probe.
	mid := len(steps) / 2

	for i, step := range steps {
		if kind == StrategyBranch && i == mid {
			if err := demo.ForkBranches("Ветка A (ТЗ)", "Ветка B (шум)"); err != nil {
				side.Error = err.Error()
				side.DurationMs = time.Since(start).Milliseconds()
				return side
			}
			// В ветке B уводим контекст в сторону.
			_ = demo.SwitchBranch("b")
			_, _ = demo.Handle(ctx, Request{
				Message:  "Забудь про еду. Теперь проектируем игру про котиков в космосе, бюджет 100 рублей.",
				Provider: provider,
			})
			_ = demo.SwitchBranch("a")
		}
		res, err := demo.Handle(ctx, Request{Message: step, Provider: provider})
		if err != nil {
			side.Error = err.Error()
			side.DurationMs = time.Since(start).Milliseconds()
			side.Session = demo.SessionTotals()
			return side
		}
		side.Turns++
		_ = res
	}

	final, err := demo.Handle(ctx, Request{Message: tzProbe, Provider: provider})
	side.DurationMs = time.Since(start).Milliseconds()
	side.Session = demo.SessionTotals()
	if final.Strategy != nil {
		side.Strategy = final.Strategy
	}
	if demo.memory != nil {
		side.Facts = demo.memory.Facts()
	}
	if err != nil {
		side.Error = err.Error()
		return side
	}
	side.FinalReply = final.Reply
	side.ProbeOK, side.ProbeNotes = probeScore(final.Reply)
	return side
}

func formatStrategyReport(r StrategyCompareResult) string {
	var b strings.Builder
	b.WriteString("Day10 — сравнение стратегий контекста (сценарий «собираем ТЗ»)\n")
	b.WriteString(strings.Repeat("=", 56) + "\n\n")
	for _, s := range r.Sides {
		b.WriteString(fmt.Sprintf("### %s (%s)\n", s.Title, s.Kind))
		if s.Error != "" {
			b.WriteString("Ошибка: " + s.Error + "\n\n")
			continue
		}
		b.WriteString(fmt.Sprintf("Токены сессии: %d (prompt %d / completion %d) · ≈$%.6f · %d ms\n",
			s.Session.TotalTokens, s.Session.PromptTokens, s.Session.CompletionTokens,
			s.Session.EstimatedCostUSD, s.DurationMs))
		b.WriteString(fmt.Sprintf("Стабильность фактов: %v — %s\n", s.ProbeOK, s.ProbeNotes))
		if len(s.Facts) > 0 {
			b.WriteString("Facts: ")
			keys := make([]string, 0, len(s.Facts))
			for k := range s.Facts {
				keys = append(keys, k)
			}
			b.WriteString(strings.Join(keys, ", "))
			b.WriteString("\n")
		}
		b.WriteString("Финальный ответ:\n")
		b.WriteString(s.FinalReply)
		b.WriteString("\n\n")
	}
	b.WriteString("Краткий вывод:\n")
	b.WriteString("- Sliding Window: дёшево по токенам, но легко теряет ранние договорённости при маленьком N.\n")
	b.WriteString("- Sticky Facts: дороже (extra LLM на facts), зато лучше держит цель/ограничения/числа.\n")
	b.WriteString("- Branching: удобно развивать альтернативы; без переключения легко «загрязнить» контекст другой веткой.\n")
	return b.String()
}