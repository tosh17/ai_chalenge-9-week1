package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/tokens"
)

// Character — участник диалога со своим system-промптом.
type Character struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
}

// DialogueRequest — сцена для двух персонажей.
// Turns опционален: 0 = пока клиент не остановит (шаговый API) или дефолт для batch.
type DialogueRequest struct {
	A        Character      `json:"a"`
	B        Character      `json:"b"`
	Topic    string         `json:"topic"`
	Turns    int            `json:"turns,omitempty"`
	Past     []DialogueTurn `json:"past,omitempty"` // уже сказанные реплики (для шага)
	Provider string         `json:"provider"`
}

// DialogueTurn — одна реплика в общем чате.
type DialogueTurn struct {
	Index      int           `json:"index"`
	Speaker    string        `json:"speaker"`
	Content    string        `json:"content"`
	DurationMs int64         `json:"duration_ms"`
	Tokens     *tokens.Usage `json:"tokens,omitempty"`
	Provider   string        `json:"provider,omitempty"`
	Model      string        `json:"model,omitempty"`
}

// DialogueResult — полный прогон или один шаг.
type DialogueResult struct {
	Turns    []DialogueTurn `json:"turns"`
	Done     bool           `json:"done,omitempty"`
	Provider string         `json:"provider"`
	Model    string         `json:"model,omitempty"`
}

func normalizeDialogue(req *DialogueRequest) error {
	req.A.Name = strings.TrimSpace(req.A.Name)
	req.B.Name = strings.TrimSpace(req.B.Name)
	req.A.Prompt = strings.TrimSpace(req.A.Prompt)
	req.B.Prompt = strings.TrimSpace(req.B.Prompt)
	req.Topic = strings.TrimSpace(req.Topic)

	if req.A.Name == "" {
		req.A.Name = "Алиса"
	}
	if req.B.Name == "" {
		req.B.Name = "Боб"
	}
	if req.A.Prompt == "" || req.B.Prompt == "" {
		return fmt.Errorf("оба персонажа должны иметь промпт")
	}
	if req.Topic == "" {
		return fmt.Errorf("нужна тема / первая реплика сцены")
	}
	return nil
}

// RunDialogueStep генерирует следующую реплику после Past.
func (a *Agent) RunDialogueStep(ctx context.Context, req DialogueRequest) (DialogueResult, error) {
	if err := normalizeDialogue(&req); err != nil {
		return DialogueResult{}, err
	}

	provider := req.Provider
	if provider == "" {
		provider = a.defaultID
	}

	histA, histB := rebuildHistories(req)
	i := len(req.Past)

	var (
		speaker, system string
		hist            *[]deepseek.Message
		peerHist        *[]deepseek.Message
	)
	if i%2 == 0 {
		speaker = req.A.Name
		system = buildCharacterSystem(req.A, req.B.Name)
		hist = &histA
		peerHist = &histB
	} else {
		speaker = req.B.Name
		system = buildCharacterSystem(req.B, req.A.Name)
		hist = &histB
		peerHist = &histA
	}

	res, err := a.Complete(ctx, provider, system, *hist)
	if err != nil {
		return DialogueResult{Provider: provider}, fmt.Errorf("turn %d (%s): %w", i+1, speaker, err)
	}
	reply := strings.TrimSpace(res.Reply)
	if reply == "" {
		return DialogueResult{Provider: provider}, fmt.Errorf("turn %d (%s): empty reply", i+1, speaker)
	}

	_ = peerHist // histories only needed for Complete input; next step rebuilds from Past

	turn := DialogueTurn{
		Index:      i + 1,
		Speaker:    speaker,
		Content:    reply,
		DurationMs: res.DurationMs,
		Tokens:     res.Tokens,
		Provider:   res.Provider,
		Model:      res.Model,
	}
	return DialogueResult{
		Turns:    []DialogueTurn{turn},
		Provider: provider,
		Model:    res.Model,
	}, nil
}

// RunDialogue — пакетный прогон (для совместимости / тестов).
func (a *Agent) RunDialogue(ctx context.Context, req DialogueRequest) (DialogueResult, error) {
	if err := normalizeDialogue(&req); err != nil {
		return DialogueResult{}, err
	}
	if req.Turns <= 0 {
		req.Turns = 8
	}
	if req.Turns > 40 {
		req.Turns = 40
	}

	provider := req.Provider
	if provider == "" {
		provider = a.defaultID
	}
	out := DialogueResult{Provider: provider, Turns: make([]DialogueTurn, 0, req.Turns)}
	past := append([]DialogueTurn{}, req.Past...)

	for len(past) < req.Turns {
		stepReq := req
		stepReq.Past = past
		step, err := a.RunDialogueStep(ctx, stepReq)
		if err != nil {
			out.Turns = past
			return out, err
		}
		if len(step.Turns) == 0 {
			break
		}
		past = append(past, step.Turns[0])
		out.Model = step.Model
		out.Turns = append(out.Turns, step.Turns[0])
	}
	out.Done = true
	return out, nil
}

func rebuildHistories(req DialogueRequest) (histA, histB []deepseek.Message) {
	seed := fmt.Sprintf(
		"Сцена/тема разговора: %s\nНачни диалог с персонажем «%s». Одна короткая реплика, без кавычек и без имени в начале.",
		req.Topic, req.B.Name,
	)
	histA = []deepseek.Message{{Role: "user", Content: seed}}

	for _, t := range req.Past {
		content := strings.TrimSpace(t.Content)
		if content == "" {
			continue
		}
		if t.Speaker == req.A.Name {
			histA = append(histA, deepseek.Message{Role: "assistant", Content: content})
			histB = append(histB, deepseek.Message{
				Role:    "user",
				Content: fmt.Sprintf("%s: %s", t.Speaker, content),
			})
		} else {
			histB = append(histB, deepseek.Message{Role: "assistant", Content: content})
			histA = append(histA, deepseek.Message{
				Role:    "user",
				Content: fmt.Sprintf("%s: %s", t.Speaker, content),
			})
		}
	}
	return histA, histB
}

func buildCharacterSystem(self Character, partnerName string) string {
	return fmt.Sprintf(
		`Ты — %s.
%s

Ты разговариваешь с персонажем «%s».
Правила:
- Оставайся в роли.
- Отвечай одной репликой (1–4 предложения), живо и по делу.
- Не пиши своё имя в начале строки.
- Не описывай сцену от автора — только речь персонажа.`,
		self.Name, self.Prompt, partnerName,
	)
}
