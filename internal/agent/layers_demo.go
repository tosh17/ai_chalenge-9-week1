package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tosh17/deepseek-service/internal/memory"
)

// MemoryDemoSide — один прогон с разной политикой подмешивания слоёв.
type MemoryDemoSide struct {
	Title      string       `json:"title"`
	Policy     MemoryPolicy `json:"policy"`
	ProbeReply string       `json:"probe_reply"`
	Hits       []string     `json:"hits"`
	Misses     []string     `json:"misses"`
	Error      string       `json:"error,omitempty"`
	DurationMs int64        `json:"duration_ms"`
	Memory     *MemoryInfo  `json:"memory,omitempty"`
}

// MemoryDemoResult — сценарий: что в каждом слое и как это меняет ответ.
type MemoryDemoResult struct {
	Provider string           `json:"provider"`
	Note     string           `json:"note,omitempty"`
	Scenario []string         `json:"scenario"`
	Probe    string           `json:"probe"`
	Seed     MemoryInfo       `json:"seed"`
	Sides    []MemoryDemoSide `json:"sides"`
	Report   string           `json:"report"`
}

var memoryDemoSeed = []struct {
	msg string
}{
	{msg: "Меня зовут Тоша, предпочитаю краткие ответы списком."},
	{msg: "Задача: собрать ТЗ на FoodDash. Бюджет 2 млн, срок 3 месяца, доставка в радиусе 5 км."},
	{msg: "Решение: backend на Go, клиенты на Flutter. Запомни это."},
	{msg: "#знание контакт: Анна, созвоны по вторникам в 11:00"},
}

var memoryDemoFillers = []string{
	"Какая сегодня погода для доставки?",
	"Напомни формат ответа — коротко.",
	"Есть ли смысл добавить чаевые?",
	"Как назвать экран корзины?",
	"Нужен ли тёмный UI на старте?",
	"Что с push-уведомлениями — позже?",
	"Ок, это пока не про ТЗ, просто болтаем.",
	"Ещё одна реплика-филлер, чтобы вытеснить STM.",
}

const memoryDemoProbe = "Ответь списком: 1) как меня зовут? 2) какой бюджет и название проекта? 3) какой стек решили? 4) кто контакт Анна? 5) что было в самом первом сообщении диалога дословно? Если слоя нет или факт вытеснен — так и скажи."

type demoCheck struct {
	name string
	need []string
}

func memoryDemoChecks() []demoCheck {
	return []demoCheck{
		{name: "имя Тоша (LTM профиль)", need: []string{"тоша"}},
		{name: "FoodDash / бюджет (WM задача)", need: []string{"fooddash", "2"}},
		{name: "стек Go/Flutter (LTM решение)", need: []string{"go", "flutter"}},
		{name: "контакт Анна (LTM знание)", need: []string{"анна"}},
	}
}

func scoreDemoReply(reply string, checks []demoCheck) (hits, misses []string) {
	low := strings.ToLower(reply)
	for _, c := range checks {
		ok := true
		for _, n := range c.need {
			if !strings.Contains(low, n) {
				ok = false
				break
			}
		}
		if ok {
			hits = append(hits, c.name)
		} else {
			misses = append(misses, c.name)
		}
	}
	return hits, misses
}

// RunMemoryDemo сеет три слоя, вытесняет STM и спрашивает одно и то же при разном inject.
func (a *Agent) RunMemoryDemo(ctx context.Context, providerID string) (MemoryDemoResult, error) {
	live, note, err := a.resolveLiveProvider(ctx, providerID)
	if err != nil {
		return MemoryDemoResult{}, err
	}
	providerID = live
	dir, err := os.MkdirTemp("", "day11-memory-demo-*")
	if err != nil {
		return MemoryDemoResult{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	layers, err := memory.OpenLayers(filepath.Join(dir, "layers"))
	if err != nil {
		return MemoryDemoResult{}, err
	}

	demo := a.CloneWithLayers("day11-demo", layers).WithProfiles(nil)
	demo.SetMemoryPolicy(MemoryPolicy{STMWindowN: 6, InjectSTM: true, InjectWM: true, InjectLTM: true})

	scenario := make([]string, 0, len(memoryDemoSeed)+len(memoryDemoFillers)+1)
	for _, s := range memoryDemoSeed {
		scenario = append(scenario, s.msg)
		if _, err := demo.Handle(ctx, Request{Message: s.msg, Provider: providerID}); err != nil {
			return MemoryDemoResult{}, fmt.Errorf("seed: %w", err)
		}
	}
	for _, s := range memoryDemoFillers {
		scenario = append(scenario, "(filler) "+s)
		if _, err := demo.Handle(ctx, Request{Message: s, Provider: providerID}); err != nil {
			return MemoryDemoResult{}, fmt.Errorf("filler: %w", err)
		}
	}

	out := MemoryDemoResult{
		Provider: providerID,
		Note:     note,
		Scenario: scenario,
		Probe:    memoryDemoProbe,
		Seed:     demo.MemorySnapshot(),
	}

	policies := []struct {
		title string
		p     MemoryPolicy
	}{
		{"все слои", MemoryPolicy{STMWindowN: 6, InjectSTM: true, InjectWM: true, InjectLTM: true, InjectProfile: true}},
		{"без STM (только WM+LTM)", MemoryPolicy{STMWindowN: 6, InjectSTM: false, InjectWM: true, InjectLTM: true, InjectProfile: true}},
		{"без WM (STM+LTM)", MemoryPolicy{STMWindowN: 6, InjectSTM: true, InjectWM: false, InjectLTM: true, InjectProfile: true}},
		{"без LTM (STM+WM)", MemoryPolicy{STMWindowN: 6, InjectSTM: true, InjectWM: true, InjectLTM: false, InjectProfile: true}},
	}

	checks := memoryDemoChecks()
	var b strings.Builder
	b.WriteString("День 11 · влияние слоёв памяти на ответ\n")
	b.WriteString("Провайдер: ")
	b.WriteString(providerID)
	b.WriteByte('\n')
	if note != "" {
		b.WriteString(note)
		b.WriteByte('\n')
	}
	b.WriteString("После посева профиля/задачи/решений STM-окно=6 вытеснило начало диалога.\n")
	b.WriteString("Один и тот же probe при разном inject:\n\n")

	for _, side := range policies {
		demo.SetMemoryPolicy(side.p)
		start := time.Now()
		res, err := demo.Handle(ctx, Request{Message: memoryDemoProbe, Provider: providerID})
		item := MemoryDemoSide{Title: side.title, Policy: side.p, DurationMs: time.Since(start).Milliseconds()}
		if err != nil {
			item.Error = err.Error()
			item.Hits, item.Misses = nil, []string{"ошибка вызова"}
		} else {
			item.ProbeReply = res.Reply
			item.Memory = res.Memory
			item.Hits, item.Misses = scoreDemoReply(res.Reply, checks)
		}
		out.Sides = append(out.Sides, item)
		b.WriteString("### ")
		b.WriteString(side.title)
		b.WriteString("\n")
		if item.Error != "" {
			b.WriteString("ошибка: ")
			b.WriteString(item.Error)
			b.WriteByte('\n')
		} else {
			b.WriteString("попало: ")
			b.WriteString(strings.Join(item.Hits, "; "))
			b.WriteString("\nне попало: ")
			if len(item.Misses) == 0 {
				b.WriteString("—")
			} else {
				b.WriteString(strings.Join(item.Misses, "; "))
			}
			b.WriteByte('\n')
			b.WriteString(truncateRunes(item.ProbeReply, 500))
			b.WriteString("\n")
		}
		b.WriteByte('\n')
	}

	b.WriteString("Ожидание: без LTM пропадает имя/стек/Анна; без WM — бюджет/FoodDash (если STM уже вытеснил); без STM пропадает дословное начало, но профиль и задача остаются.\n")
	out.Report = b.String()
	return out, nil
}

type pinger interface {
	Ping(context.Context) error
}

func (a *Agent) resolveLiveProvider(ctx context.Context, want string) (id, note string, err error) {
	if want == "" {
		want = a.defaultID
	}
	order := make([]string, 0, len(a.order)+1)
	seen := map[string]bool{}
	for _, id := range append([]string{want}, a.order...) {
		if id == "" || seen[id] {
			continue
		}
		if _, ok := a.backends[id]; !ok {
			continue
		}
		seen[id] = true
		order = append(order, id)
	}
	var failed []string
	for _, id := range order {
		p, ok := a.backends[id].llm.(pinger)
		if !ok {
			return id, fallbackNote(want, id), nil
		}
		pingCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		perr := p.Ping(pingCtx)
		cancel()
		if perr == nil {
			return id, fallbackNote(want, id), nil
		}
		failed = append(failed, fmt.Sprintf("%s: %v", id, perr))
	}
	if len(failed) == 0 {
		return "", "", fmt.Errorf("нет зарегистрированных LLM")
	}
	return "", "", fmt.Errorf("нет доступного LLM (%s)", strings.Join(failed, "; "))
}

func fallbackNote(want, got string) string {
	if want == got || want == "" {
		return ""
	}
	return fmt.Sprintf("%s недоступен, демо идёт через %s", want, got)
}
