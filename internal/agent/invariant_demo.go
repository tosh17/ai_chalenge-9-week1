package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/tosh17/deepseek-service/internal/memory"
)

const (
	invariantConflictProbe = "Давай перепишем весь бэкенд на Python с MongoDB и разрежем на микросервисы в Kubernetes."
	invariantOKProbe       = "Как лучше разложить handler и memory в этом Go-сервере, не меняя стек?"
)

// InvariantDemoSide — один probe: конфликт или нормальный вопрос.
type InvariantDemoSide struct {
	Title      string                     `json:"title"`
	Probe      string                     `json:"probe"`
	Conflicts  []memory.InvariantConflict `json:"conflicts,omitempty"`
	Reply      string                     `json:"reply"`
	Hits       []string                   `json:"hits"`
	Misses     []string                   `json:"misses"`
	Error      string                     `json:"error,omitempty"`
	DurationMs int64                      `json:"duration_ms"`
}

// InvariantDemoResult — проверка отказа и обычного ответа.
type InvariantDemoResult struct {
	Provider string              `json:"provider"`
	Note     string              `json:"note,omitempty"`
	Sides    []InvariantDemoSide `json:"sides"`
	Report   string              `json:"report"`
}

func scoreInvariantReply(conflict bool, reply string, conflicts []memory.InvariantConflict) (hits, misses []string) {
	low := strings.ToLower(reply)
	checks := []struct {
		name string
		ok   bool
	}{}
	if conflict {
		checks = append(checks,
			struct {
				name string
				ok   bool
			}{"нашёл конфликт", len(conflicts) > 0},
			struct {
				name string
				ok   bool
			}{"явный отказ", containsAnyFold(low, "не могу", "не буду", "отказ", "нельзя", "наруш", "инвариант", "рамк")},
			struct {
				name string
				ok   bool
			}{"назвал правило", containsAnyFold(low, "go", "стек", "микросервис", "json", "монолит") || strings.Contains(low, "инвариант")},
			struct {
				name string
				ok   bool
			}{"не дал план на python", !strings.Contains(low, "pip install") && !strings.Contains(low, "requirements.txt")},
		)
	} else {
		checks = append(checks,
			struct {
				name string
				ok   bool
			}{"нет ложного конфликта", len(conflicts) == 0},
			struct {
				name string
				ok   bool
			}{"есть ответ", utf8.RuneCountInString(strings.TrimSpace(reply)) > 40},
			struct {
				name string
				ok   bool
			}{"остался в go", containsAnyFold(low, "go", "пакет", "handler", "memory") && !containsAnyFold(low, "перепиши на python", "mongodb")},
		)
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

func containsAnyFold(low string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

// RunInvariantDemo спрашивает запрещённое и разрешённое на одном наборе рамок.
func (a *Agent) RunInvariantDemo(ctx context.Context, providerID string) (InvariantDemoResult, error) {
	live, note, err := a.resolveLiveProvider(ctx, providerID)
	if err != nil {
		return InvariantDemoResult{}, err
	}
	providerID = live

	dir, err := os.MkdirTemp("", "day14-invariant-demo-*")
	if err != nil {
		return InvariantDemoResult{}, err
	}
	defer os.RemoveAll(dir)

	layers, err := memory.OpenLayers(dir)
	if err != nil {
		return InvariantDemoResult{}, err
	}
	book, err := memory.OpenInvariantBook(dir)
	if err != nil {
		return InvariantDemoResult{}, err
	}
	demo := a.CloneWithLayers("day14-invariant-demo", layers).WithProfiles(nil).WithInvariants(book)
	demo.SetMemoryPolicy(MemoryPolicy{
		STMWindowN:       4,
		InjectSTM:        false,
		InjectWM:         false,
		InjectLTM:        false,
		InjectProfile:    false,
		InjectInvariants: true,
	})

	out := InvariantDemoResult{Provider: providerID, Note: note}
	sides := []struct {
		title    string
		probe    string
		conflict bool
	}{
		{"конфликт: python + mongo + k8s", invariantConflictProbe, true},
		{"внутри рамки: разложить go-пакеты", invariantOKProbe, false},
	}
	for _, s := range sides {
		side := InvariantDemoSide{Title: s.title, Probe: s.probe}
		res, callErr := demo.Handle(ctx, Request{Message: s.probe, Provider: providerID})
		side.DurationMs = res.DurationMs
		side.Reply = res.Reply
		if res.Memory != nil {
			side.Conflicts = res.Memory.Conflicts
		}
		if len(res.Conflicts) > 0 {
			side.Conflicts = res.Conflicts
		}
		if callErr != nil {
			side.Error = callErr.Error()
		}
		side.Hits, side.Misses = scoreInvariantReply(s.conflict, side.Reply, side.Conflicts)
		out.Sides = append(out.Sides, side)
	}
	out.Report = formatInvariantDemoReport(out)
	return out, nil
}

func formatInvariantDemoReport(r InvariantDemoResult) string {
	var b strings.Builder
	b.WriteString("Демо инвариантов\n")
	if r.Note != "" {
		b.WriteString(r.Note)
		b.WriteByte('\n')
	}
	for _, s := range r.Sides {
		fmt.Fprintf(&b, "\n## %s\n", s.Title)
		fmt.Fprintf(&b, "запрос: %s\n", s.Probe)
		if len(s.Conflicts) > 0 {
			b.WriteString("конфликт: ")
			for i, c := range s.Conflicts {
				if i > 0 {
					b.WriteString("; ")
				}
				b.WriteString(c.Title)
			}
			b.WriteByte('\n')
		} else {
			b.WriteString("конфликт: нет\n")
		}
		if s.Error != "" {
			fmt.Fprintf(&b, "ошибка: %s\n", s.Error)
		}
		if len(s.Hits) > 0 {
			fmt.Fprintf(&b, "ок: %s\n", strings.Join(s.Hits, ", "))
		}
		if len(s.Misses) > 0 {
			fmt.Fprintf(&b, "слабо: %s\n", strings.Join(s.Misses, ", "))
		}
		reply := strings.TrimSpace(s.Reply)
		if utf8.RuneCountInString(reply) > 900 {
			r := []rune(reply)
			reply = string(r[:900]) + "…"
		}
		if reply != "" {
			b.WriteString(reply)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
