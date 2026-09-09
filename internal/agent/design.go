package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DesignSpec — результат работы агента-дизайнера (тема для фронта).
type DesignSpec struct {
	ThemeName     string            `json:"theme_name"`
	Title         string            `json:"title"`
	Subtitle      string            `json:"subtitle"`
	WelcomeTitle  string            `json:"welcome_title"`
	WelcomeText   string            `json:"welcome_text"`
	LogoText      string            `json:"logo_text"`
	Colors        map[string]string `json:"colors"`
	FontFamily    string            `json:"font_family"`
	BorderWidth   string            `json:"border_width"`
	Radius        string            `json:"radius"`
}

// DesignStep — шаг пайплайна для отладки/UI.
type DesignStep struct {
	Agent  string `json:"agent"`
	Output string `json:"output"`
}

// DesignResult — итог цепочки Prompt → Designer → Apply.
type DesignResult struct {
	Steps      []DesignStep       `json:"steps"`
	Prompt     string             `json:"prompt"`
	Design     DesignSpec         `json:"design"`
	CSSVars    map[string]string  `json:"css_vars"`
	Provider   string             `json:"provider"`
}

// DesignCrew — три агента: промптер, дизайнер и применитель темы.
type DesignCrew struct {
	promptAgent   *Agent
	designerAgent *Agent
	applier       *ThemeApplier
}

// NewDesignCrew собирает команду поверх уже настроенного агента с бэкендами.
func NewDesignCrew(base *Agent) *DesignCrew {
	prompt := New("prompt-agent")
	prompt.systemPrompt = `Ты агент-промптер для дизайна веб-интерфейса чата.
По короткому пожеланию пользователя составь ОДИН развёрнутый дизайн-промпт на русском.
Укажи: настроение, цвета, типографику, форму кнопок/бабблов, фон, акценты.
Не возвращай JSON. Только текст промпта.`
	prompt.backends = base.backends
	prompt.order = append([]string{}, base.order...)
	prompt.defaultID = base.defaultID

	designer := New("designer-agent")
	designer.systemPrompt = `Ты агент-дизайнер UI. По дизайн-промпту верни ТОЛЬКО валидный JSON без markdown и пояснений.
Схема:
{
  "theme_name": "string",
  "title": "string",
  "subtitle": "string",
  "welcome_title": "string",
  "welcome_text": "string",
  "logo_text": "string",
  "colors": {
    "bg": "#hex",
    "surface": "#hex",
    "surface_2": "#hex",
    "border": "#hex",
    "text": "#hex",
    "text_muted": "#hex",
    "accent": "#hex",
    "accent_hover": "#hex",
    "user_bg": "#hex",
    "assistant_bg": "#hex",
    "error": "#hex"
  },
  "font_family": "css font-family stack",
  "border_width": "e.g. 3px",
  "radius": "e.g. 16px"
}`
	designer.backends = base.backends
	designer.order = append([]string{}, base.order...)
	designer.defaultID = base.defaultID

	return &DesignCrew{
		promptAgent:   prompt,
		designerAgent: designer,
		applier:       NewThemeApplier(),
	}
}

// Run запускает цепочку: промпт → дизайн → применение (CSS-переменные).
func (c *DesignCrew) Run(ctx context.Context, wish, provider string) (DesignResult, error) {
	if wish == "" {
		wish = "Сделай чат в ярком мультяшном стиле американского ситкома 90-х: жёлтый, небо, толстые контуры, комикс."
	}

	out := DesignResult{Provider: provider, Steps: make([]DesignStep, 0, 3)}

	promptRes, err := c.promptAgent.Handle(ctx, Request{
		Message:  wish,
		Provider: provider,
	})
	if err != nil {
		return out, fmt.Errorf("prompt-agent: %w", err)
	}
	out.Prompt = strings.TrimSpace(promptRes.Reply)
	out.Steps = append(out.Steps, DesignStep{Agent: c.promptAgent.Name(), Output: out.Prompt})

	designRes, err := c.designerAgent.Handle(ctx, Request{
		Message:  "Дизайн-промпт:\n" + out.Prompt,
		Provider: provider,
	})
	if err != nil {
		return out, fmt.Errorf("designer-agent: %w", err)
	}
	out.Steps = append(out.Steps, DesignStep{Agent: c.designerAgent.Name(), Output: designRes.Reply})

	spec, err := parseDesignSpec(designRes.Reply)
	if err != nil {
		return out, fmt.Errorf("designer-agent parse: %w", err)
	}
	out.Design = spec

	applied, err := c.applier.Apply(spec)
	if err != nil {
		return out, fmt.Errorf("apply-agent: %w", err)
	}
	out.CSSVars = applied
	out.Steps = append(out.Steps, DesignStep{
		Agent:  "apply-agent",
		Output: fmt.Sprintf("применено %d CSS-переменных", len(applied)),
	})

	return out, nil
}

func parseDesignSpec(raw string) (DesignSpec, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	// вырезаем первый JSON-объект, если модель добавила текст
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}

	var spec DesignSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return DesignSpec{}, err
	}
	if spec.Colors == nil {
		spec.Colors = map[string]string{}
	}
	return spec, nil
}

// ThemeApplier — агент применения: превращает DesignSpec в CSS-переменные для фронта.
type ThemeApplier struct {
	name string
}

func NewThemeApplier() *ThemeApplier {
	return &ThemeApplier{name: "apply-agent"}
}

func (a *ThemeApplier) Name() string { return a.name }

func (a *ThemeApplier) Apply(spec DesignSpec) (map[string]string, error) {
	c := spec.Colors
	get := func(key, fallback string) string {
		if v := strings.TrimSpace(c[key]); v != "" {
			return v
		}
		return fallback
	}

	vars := map[string]string{
		"--bg":            get("bg", "#87CEEB"),
		"--surface":       get("surface", "#FFD90F"),
		"--surface-2":     get("surface_2", "#FFE66D"),
		"--border":        get("border", "#111111"),
		"--text":          get("text", "#111111"),
		"--text-muted":    get("text_muted", "#333333"),
		"--accent":        get("accent", "#F14E23"),
		"--accent-hover":  get("accent_hover", "#D63E18"),
		"--user-bg":       get("user_bg", "#A0D8EF"),
		"--assistant-bg":  get("assistant_bg", "#FFF7C2"),
		"--error":         get("error", "#D32F2F"),
		"--radius":        firstNonEmpty(spec.Radius, "18px"),
		"--border-width":  firstNonEmpty(spec.BorderWidth, "3px"),
		"--font-family":   firstNonEmpty(spec.FontFamily, `"Fredoka", "Comic Sans MS", "Chalkboard SE", sans-serif`),
		"--shadow":        "4px 4px 0 #111111",
	}
	return vars, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
