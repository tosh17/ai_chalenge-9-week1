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

const profileDemoProbe = "Как организовать работу двух бригад на объекте на этой неделе?"

// ProfileDemoSide — один и тот же вопрос при другом профиле.
type ProfileDemoSide struct {
	Title      string             `json:"title"`
	Profile    memory.UserProfile `json:"profile"`
	Injected   bool               `json:"injected"`
	Reply      string             `json:"reply"`
	Hits       []string           `json:"hits"`
	Misses     []string           `json:"misses"`
	Error      string             `json:"error,omitempty"`
	DurationMs int64              `json:"duration_ms"`
}

// ProfileDemoResult — сравнение персонализации.
type ProfileDemoResult struct {
	Provider string            `json:"provider"`
	Note     string            `json:"note,omitempty"`
	Probe    string            `json:"probe"`
	Sides    []ProfileDemoSide `json:"sides"`
	Report   string            `json:"report"`
}

func scoreProfileReply(id, reply string) (hits, misses []string) {
	low := strings.ToLower(reply)
	runes := utf8.RuneCountInString(strings.TrimSpace(reply))
	checks := map[string][]struct {
		name string
		ok   bool
	}{
		"brief": {
			{name: "список (дефис/нумерация)", ok: strings.Contains(reply, "\n-") || strings.Contains(reply, "\n1") || strings.Contains(reply, "•")},
			{name: "сжатость (<900 символов)", ok: runes < 900},
		},
		"exec": {
			{name: "обращение на вы", ok: strings.Contains(low, "вам") || strings.Contains(low, "вы ") || strings.Contains(low, "ваш")},
			{name: "без эмодзи", ok: !strings.ContainsAny(reply, "😀😁😂😊😉👍🔥")},
		},
		"learner": {
			{name: "пример/простыми словами", ok: strings.Contains(low, "например") || strings.Contains(low, "проще") || strings.Contains(low, "как если")},
			{name: "не слишком длинно (<1200)", ok: runes < 1200},
		},
		"none": {
			{name: "есть ответ", ok: strings.TrimSpace(reply) != ""},
		},
	}
	for _, c := range checks[id] {
		if c.ok {
			hits = append(hits, c.name)
		} else {
			misses = append(misses, c.name)
		}
	}
	return hits, misses
}

// RunProfileDemo задаёт один probe трём пресетам и без профиля.
func (a *Agent) RunProfileDemo(ctx context.Context, providerID string) (ProfileDemoResult, error) {
	live, note, err := a.resolveLiveProvider(ctx, providerID)
	if err != nil {
		return ProfileDemoResult{}, err
	}
	providerID = live

	dir, err := os.MkdirTemp("", "day12-profile-demo-*")
	if err != nil {
		return ProfileDemoResult{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	layers, err := memory.OpenLayers(filepath.Join(dir, "layers"))
	if err != nil {
		return ProfileDemoResult{}, err
	}
	book, err := memory.OpenProfileBook(filepath.Join(dir, "profiles"))
	if err != nil {
		return ProfileDemoResult{}, err
	}

	demo := a.CloneWithLayers("day12-profile-demo", layers).WithProfiles(book)
	demo.SetMemoryPolicy(MemoryPolicy{
		STMWindowN: 4, InjectSTM: false, InjectWM: false, InjectLTM: false, InjectProfile: true,
	})

	out := ProfileDemoResult{Provider: providerID, Note: note, Probe: profileDemoProbe}
	var b strings.Builder
	b.WriteString("День 12 · персонализация: один вопрос, разные профили\n")
	b.WriteString("Провайдер: ")
	b.WriteString(providerID)
	b.WriteByte('\n')
	if note != "" {
		b.WriteString(note)
		b.WriteByte('\n')
	}
	b.WriteString("Probe: ")
	b.WriteString(profileDemoProbe)
	b.WriteString("\nSTM/WM/LTM в промпт не подмешивались — виден только профиль.\n\n")

	sides := []struct {
		id       string
		title    string
		inject   bool
		activate string
	}{
		{id: "brief", title: "Бригадир · кратко", inject: true, activate: "brief"},
		{id: "exec", title: "Заказчик · формально", inject: true, activate: "exec"},
		{id: "learner", title: "Новичок · с примерами", inject: true, activate: "learner"},
		{id: "none", title: "без профиля", inject: false, activate: "brief"},
	}

	for _, side := range sides {
		if side.activate != "" {
			_ = book.Activate(side.activate)
		}
		demo.SetMemoryPolicy(MemoryPolicy{
			STMWindowN: 4, InjectSTM: false, InjectWM: false, InjectLTM: false, InjectProfile: side.inject,
		})
		item := ProfileDemoSide{Title: side.title, Profile: book.Active(), Injected: side.inject}
		if !side.inject {
			item.Profile = memory.UserProfile{}
		}
		start := time.Now()
		res, err := demo.Handle(ctx, Request{Message: profileDemoProbe, Provider: providerID})
		item.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			item.Error = err.Error()
			item.Misses = []string{"ошибка вызова"}
		} else {
			item.Reply = res.Reply
			item.Hits, item.Misses = scoreProfileReply(side.id, res.Reply)
		}
		out.Sides = append(out.Sides, item)

		b.WriteString("### ")
		b.WriteString(side.title)
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
