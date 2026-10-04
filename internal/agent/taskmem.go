package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/memory"
)

// TaskView — память задачи, которую видит чат.
type TaskView struct {
	Goal        string   `json:"goal,omitempty"`
	Clarified   []string `json:"clarified,omitempty"`
	Constraints []string `json:"constraints,omitempty"`
}

type taskUpdate struct {
	Goal        string   `json:"goal"`
	Clarified   []string `json:"clarified"`
	Constraints []string `json:"constraints"`
}

func (a *Agent) taskView() TaskView {
	if a.layers == nil {
		return TaskView{}
	}
	w := a.layers.Working()
	return TaskView{Goal: w.Goal, Clarified: append([]string{}, w.Notes...), Constraints: append([]string{}, w.Constraints...)}
}

func (a *Agent) rememberTask(ctx context.Context, llm LLM, message string) TaskView {
	prev := a.taskView()
	if a.layers == nil {
		return prev
	}
	next := prev
	chat, err := llm.Chat(ctx, []deepseek.Message{
		{Role: "system", Content: "Обнови память кулинарного диалога. Ответь только JSON {\"goal\":\"\",\"clarified\":[],\"constraints\":[]}. Если пользователь просит другое блюдо или явно меняет цель, goal — новая цель. Иначе повтори прежнюю дословно. clarified — новые уточнения. constraints — только новые запреты продуктов и условия готовки, без фраз вроде «не меняй цель». Не выдумывай. Пустые массивы, если нового нет."},
		{Role: "user", Content: "Прежняя цель: " + emptyDash(prev.Goal) + "\nУже уточнено: " + joinOrDash(prev.Clarified) + "\nОграничения: " + joinOrDash(prev.Constraints) + "\nРеплика: " + message},
	})
	if err == nil {
		if upd, ok := parseTaskUpdate(chat.Reply); ok {
			next = mergeTask(prev, upd)
		}
	}
	if next.Goal == "" && len(next.Clarified) == 0 && len(next.Constraints) == 0 {
		return prev
	}
	_ = a.layers.ApplyWorking(memory.WorkingPatch{
		Goal:        next.Goal,
		Constraints: next.Constraints,
		AddNotes:    next.Clarified,
		Status:      memory.WorkingActive,
	})
	return a.taskView()
}

func mergeTask(prev TaskView, upd taskUpdate) TaskView {
	goal := strings.TrimSpace(upd.Goal)
	if goal == "" {
		goal = prev.Goal
	}
	return TaskView{
		Goal:        goal,
		Clarified:   appendUnique(prev.Clarified, upd.Clarified...),
		Constraints: appendUnique(prev.Constraints, upd.Constraints...),
	}
}

func parseTaskUpdate(raw string) (taskUpdate, bool) {
	i := strings.Index(raw, "{")
	j := strings.LastIndex(raw, "}")
	if i < 0 || j <= i {
		return taskUpdate{}, false
	}
	var upd taskUpdate
	if err := json.Unmarshal([]byte(raw[i:j+1]), &upd); err != nil {
		return taskUpdate{}, false
	}
	return upd, true
}

func appendUnique(base []string, extra ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(base)+len(extra))
	for _, item := range append(append([]string{}, base...), extra...) {
		item = strings.TrimSpace(item)
		key := strings.ToLower(item)
		if item == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func (a *Agent) dialogContext() string {
	if a.layers == nil {
		return ""
	}
	msgs := a.layers.ShortTerm()
	var b strings.Builder
	for _, m := range msgs {
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		if m.Role == "assistant" {
			text = clip(text, 400)
		}
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func taskPrompt(task TaskView, history string) string {
	var b strings.Builder
	b.WriteString("Память задачи.\nЦель: ")
	b.WriteString(emptyDash(task.Goal))
	b.WriteString("\nУже уточнено: ")
	b.WriteString(joinOrDash(task.Clarified))
	b.WriteString("\nОграничения: ")
	b.WriteString(joinOrDash(task.Constraints))
	if history != "" {
		b.WriteString("\n\nНедавний диалог:\n")
		b.WriteString(history)
	}
	b.WriteString("\n\nЕсли пользователь меняет цель, прими новую и отвечай по ней. Не пиши, что оставляешь прежнюю цель. Если цитаты не отвечают на вопрос, но вопрос про еду, ответь из общих знаний и напиши, что это не из книги. Если вопрос не про еду и не меняет цель, скажи, что это не по теме. Запреты продуктов не отменяй, пока пользователь сам их не снимет.")
	return b.String()
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func joinOrDash(items []string) string {
	if len(items) == 0 {
		return "—"
	}
	return strings.Join(items, "; ")
}
