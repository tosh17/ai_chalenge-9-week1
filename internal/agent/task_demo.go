package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tosh17/deepseek-service/internal/memory"
)

const taskDemoProbePause = "Напомни весь план с самого начала: зачем две бригады, какой фундамент и с чего мы начинали?"
const taskDemoProbeResume = "Продолжай."

// TaskDemoTurn — один ход автомата.
type TaskDemoTurn struct {
	Title      string           `json:"title"`
	Prompt     string           `json:"prompt"`
	Task       memory.TaskState `json:"task"`
	Reply      string           `json:"reply"`
	Hits       []string         `json:"hits"`
	Misses     []string         `json:"misses"`
	Error      string           `json:"error,omitempty"`
	DurationMs int64            `json:"duration_ms"`
}

// TaskDemoResult — пауза и продолжение без повторных объяснений.
type TaskDemoResult struct {
	Provider string         `json:"provider"`
	Note     string         `json:"note,omitempty"`
	Turns    []TaskDemoTurn `json:"turns"`
	Report   string         `json:"report"`
}

// SeedExampleTask ставит живой пример на этапе execution.
func SeedExampleTask(layers *memory.Layers) error {
	return seedTaskMachine(layers)
}

func seedTaskMachine(layers *memory.Layers) error {
	if err := layers.ApplyWorking(memory.WorkingPatch{
		Goal:     "каркасный дом, две бригады на объекте",
		Event:    memory.TaskStart,
		AddNotes: []string{"смета ещё не закрыта"},
		AddDone:  []string{"план: каркас, две бригады, без воды в ответах"},
	}); err != nil {
		return err
	}
	if err := layers.ApplyTask(memory.TaskAdvance, memory.WorkingPatch{}); err != nil {
		return err
	}
	return layers.ApplyWorking(memory.WorkingPatch{
		Event:     memory.TaskSet,
		Step:      "собрать стены 1 этажа",
		StepIndex: 2,
		Expect:    "пользователь подтверждает материал стен",
		AddDone:   []string{"выбран участок и ленточный фундамент"},
	})
}

func scorePauseReply(reply string) (hits, misses []string) {
	low := strings.ToLower(reply)
	runes := utf8.RuneCountInString(strings.TrimSpace(reply))
	checks := []struct {
		name string
		ok   bool
	}{
		{name: "коротко (<500)", ok: runes < 500},
		{name: "слово паузы/жду", ok: strings.Contains(low, "пауз") || strings.Contains(low, "жду") || strings.Contains(low, "останов") || strings.Contains(low, "стоп")},
		{name: "без полного плана", ok: !strings.Contains(low, "ленточн") && !strings.Contains(low, "зачем две бригады")},
	}
	for _, c := range checks {
		if c.ok {
			hits = append(hits, c.name)
		} else {
			misses = append(misses, c.name)
		}
	}
	return hits, misses
}

func scoreResumeReply(reply string) (hits, misses []string) {
	low := strings.ToLower(reply)
	checks := []struct {
		name string
		ok   bool
	}{
		{name: "текущий шаг (стены/материал)", ok: strings.Contains(low, "стен") || strings.Contains(low, "материал")},
		{name: "без повторного введения", ok: !strings.Contains(low, "итак, начнём с плана") && !strings.Contains(low, "напомню весь план")},
	}
	for _, c := range checks {
		if c.ok {
			hits = append(hits, c.name)
		} else {
			misses = append(misses, c.name)
		}
	}
	return hits, misses
}

// RunTaskDemo: пауза на execution, затем продолжение без пересказа плана.
func (a *Agent) RunTaskDemo(ctx context.Context, providerID string) (TaskDemoResult, error) {
	live, note, err := a.resolveLiveProvider(ctx, providerID)
	if err != nil {
		return TaskDemoResult{}, err
	}
	providerID = live

	dir, err := os.MkdirTemp("", "day13-task-demo-*")
	if err != nil {
		return TaskDemoResult{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	layers, err := memory.OpenLayers(filepath.Join(dir, "layers"))
	if err != nil {
		return TaskDemoResult{}, err
	}
	if err := seedTaskMachine(layers); err != nil {
		return TaskDemoResult{}, err
	}

	demo := a.CloneWithLayers("day13-task-demo", layers).WithProfiles(nil)
	demo.SetMemoryPolicy(MemoryPolicy{
		STMWindowN: 6, InjectSTM: true, InjectWM: true, InjectLTM: false, InjectProfile: false,
	})

	out := TaskDemoResult{Provider: providerID, Note: note}
	var b strings.Builder
	b.WriteString("День 13 · конечный автомат задачи\n")
	b.WriteString("Этапы: planning → execution → validation → done\n")
	b.WriteString("Провайдер: ")
	b.WriteString(providerID)
	b.WriteByte('\n')
	if note != "" {
		b.WriteString(note)
		b.WriteByte('\n')
	}
	b.WriteString("Посев: execution / шаг «стены 1 этажа» / уже сделано: план и фундамент.\n\n")

	type step struct {
		title string
		setup func() error
		probe string
		score func(string) ([]string, []string)
	}
	steps := []step{
		{
			title: "пауза на execution",
			setup: func() error { return layers.ApplyTask(memory.TaskPause, memory.WorkingPatch{}) },
			probe: taskDemoProbePause,
			score: scorePauseReply,
		},
		{
			title: "продолжение без повтора",
			setup: func() error { return layers.ApplyTask(memory.TaskResume, memory.WorkingPatch{}) },
			probe: taskDemoProbeResume,
			score: scoreResumeReply,
		},
	}

	for _, s := range steps {
		if err := s.setup(); err != nil {
			return out, err
		}
		item := TaskDemoTurn{Title: s.title, Prompt: s.probe, Task: layers.Working().Task}
		start := time.Now()
		res, err := demo.Handle(ctx, Request{Message: s.probe, Provider: providerID})
		item.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			item.Error = err.Error()
			item.Misses = []string{"ошибка вызова"}
		} else {
			item.Reply = res.Reply
			item.Hits, item.Misses = s.score(res.Reply)
			item.Task = layers.Working().Task
		}
		out.Turns = append(out.Turns, item)

		b.WriteString("### ")
		b.WriteString(s.title)
		b.WriteString(" · этап=")
		b.WriteString(item.Task.Stage)
		if item.Task.Paused {
			b.WriteString(" · ПАУЗА")
		}
		b.WriteByte('\n')
		b.WriteString("шаг: ")
		b.WriteString(item.Task.Step)
		b.WriteString("\nожидаю: ")
		b.WriteString(item.Task.Expect)
		b.WriteByte('\n')
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
			b.WriteString(item.Reply)
			b.WriteString("\n")
		}
		b.WriteByte('\n')
	}

	out.Report = b.String()
	return out, nil
}
