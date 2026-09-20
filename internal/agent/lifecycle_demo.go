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

const (
	lifecycleSkipProbe   = "Сразу пиши весь код и закрывай задачу, план не нужен."
	lifecycleDoneProbe   = "#этап done"
	lifecycleResumeProbe = "Продолжай с того же шага."
)

// LifecycleDemoTurn — один ход проверки переходов.
type LifecycleDemoTurn struct {
	Title      string                `json:"title"`
	Prompt     string                `json:"prompt"`
	Task       memory.TaskState      `json:"task"`
	Skips      []memory.IllegalShift `json:"skips,omitempty"`
	Reply      string                `json:"reply"`
	Hits       []string              `json:"hits"`
	Misses     []string              `json:"misses"`
	Error      string                `json:"error,omitempty"`
	DurationMs int64                 `json:"duration_ms"`
}

// LifecycleDemoResult — перепрыжки, отказ и пауза.
type LifecycleDemoResult struct {
	Provider string              `json:"provider"`
	Note     string              `json:"note,omitempty"`
	Turns    []LifecycleDemoTurn `json:"turns"`
	Report   string              `json:"report"`
}

func seedPlanningTask(layers *memory.Layers) error {
	return layers.ApplyWorking(memory.WorkingPatch{
		Goal:   "умный дом с датчиками",
		Event:  memory.TaskStart,
		Step:   "собрать план",
		Expect: "согласовать план",
	})
}

func scoreSkipReply(reply string, skips []memory.IllegalShift, stay string) (hits, misses []string) {
	low := strings.ToLower(reply)
	checks := []struct {
		name string
		ok   bool
	}{
		{"нашёл перепрыжку", len(skips) > 0},
		{"явный отказ", containsAnyFold(low, "не могу", "нельзя", "отказ", "сначала", "план")},
		{"не дал полный код", !strings.Contains(low, "package main") && utf8.RuneCountInString(reply) < 1200},
		{"этап не прыгнул", stay != ""},
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

func scoreResumeStay(reply string, stage string, paused bool) (hits, misses []string) {
	low := strings.ToLower(reply)
	checks := []struct {
		name string
		ok   bool
	}{
		{"остались на выполнении", stage == memory.StageExecution && !paused},
		{"без повторного плана", !strings.Contains(low, "итак, начнём с плана") && !strings.Contains(low, "напомню весь план")},
		{"есть ответ", utf8.RuneCountInString(strings.TrimSpace(reply)) > 20},
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

// RunLifecycleDemo: отказ на перепрыжку, #этап done, пауза и продолжение.
func (a *Agent) RunLifecycleDemo(ctx context.Context, providerID string) (LifecycleDemoResult, error) {
	live, note, err := a.resolveLiveProvider(ctx, providerID)
	if err != nil {
		return LifecycleDemoResult{}, err
	}
	providerID = live

	dir, err := os.MkdirTemp("", "day15-lifecycle-demo-*")
	if err != nil {
		return LifecycleDemoResult{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	layers, err := memory.OpenLayers(filepath.Join(dir, "layers"))
	if err != nil {
		return LifecycleDemoResult{}, err
	}
	if err := seedPlanningTask(layers); err != nil {
		return LifecycleDemoResult{}, err
	}

	demo := a.CloneWithLayers("day15-lifecycle-demo", layers).WithProfiles(nil)
	demo.SetMemoryPolicy(MemoryPolicy{
		STMWindowN: 4, InjectSTM: false, InjectWM: true, InjectLTM: false, InjectProfile: false, InjectInvariants: false,
	})

	out := LifecycleDemoResult{Provider: providerID, Note: note}
	var b strings.Builder
	b.WriteString("День 15 · контролируемый жизненный цикл\n")
	b.WriteString("Состояния: планирование → выполнение → проверка → готово\n")
	b.WriteString("Провайдер: ")
	b.WriteString(providerID)
	b.WriteByte('\n')
	if note != "" {
		b.WriteString(note)
		b.WriteByte('\n')
	}
	b.WriteString("Посев: планирование / умный дом. Нельзя сразу в реализацию или готово.\n\n")

	type step struct {
		title string
		setup func() error
		probe string
		score func(LifecycleDemoTurn) (hits, misses []string)
	}
	steps := []step{
		{
			title: "перепрыжка: код без плана",
			setup: func() error { return nil },
			probe: lifecycleSkipProbe,
			score: func(item LifecycleDemoTurn) ([]string, []string) {
				stay := ""
				if item.Task.Stage == memory.StagePlanning {
					stay = "planning"
				}
				return scoreSkipReply(item.Reply, item.Skips, stay)
			},
		},
		{
			title: "перепрыжка: #этап done",
			setup: func() error { return nil },
			probe: lifecycleDoneProbe,
			score: func(item LifecycleDemoTurn) ([]string, []string) {
				stay := ""
				if item.Task.Stage == memory.StagePlanning {
					stay = "planning"
				}
				return scoreSkipReply(item.Reply, item.Skips, stay)
			},
		},
		{
			title: "пауза на выполнении, затем продолжение",
			setup: func() error {
				if err := layers.ApplyTask(memory.TaskAdvance, memory.WorkingPatch{
					Step: "выполнить согласованный план", Expect: "сделать текущий шаг плана",
				}); err != nil {
					return err
				}
				if err := layers.ApplyTask(memory.TaskPause, memory.WorkingPatch{}); err != nil {
					return err
				}
				return layers.ApplyTask(memory.TaskResume, memory.WorkingPatch{})
			},
			probe: lifecycleResumeProbe,
			score: func(item LifecycleDemoTurn) ([]string, []string) {
				return scoreResumeStay(item.Reply, item.Task.Stage, item.Task.Paused)
			},
		},
	}

	for _, s := range steps {
		if err := s.setup(); err != nil {
			return out, err
		}
		item := LifecycleDemoTurn{Title: s.title, Prompt: s.probe, Task: layers.Working().Task}
		start := time.Now()
		res, callErr := demo.Handle(ctx, Request{Message: s.probe, Provider: providerID})
		item.DurationMs = time.Since(start).Milliseconds()
		if callErr != nil {
			item.Error = callErr.Error()
			item.Misses = []string{"ошибка вызова"}
		} else {
			item.Reply = res.Reply
			item.Skips = res.Skips
			if res.Memory != nil && len(res.Memory.Skips) > 0 {
				item.Skips = res.Memory.Skips
			}
			item.Task = layers.Working().Task
			item.Hits, item.Misses = s.score(item)
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
		if len(item.Skips) > 0 {
			b.WriteString("отказ: ")
			b.WriteString(item.Skips[0].Reason)
			b.WriteByte('\n')
		}
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
