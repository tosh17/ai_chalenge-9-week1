package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const invariantsFileName = "invariants.json"

// Виды инвариантов — жёсткие рамки, которые ассистент не имеет права нарушать.
const (
	InvariantArchitecture = "architecture"
	InvariantDecision     = "decision"
	InvariantStack        = "stack"
	InvariantBusiness     = "business"
)

// Invariant — правило вне диалога: архитектура, решение, стек, бизнес-правило.
type Invariant struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Rule    string   `json:"rule"`
	Forbid  []string `json:"forbid,omitempty"`
	Enabled bool     `json:"enabled"`
}

// InvariantConflict — запрос противоречит конкретному инварианту.
type InvariantConflict struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Rule   string `json:"rule"`
	Hit    string `json:"hit,omitempty"`
	Reason string `json:"reason"`
}

type invariantsFile struct {
	Items []Invariant `json:"items"`
}

// InvariantBook — отдельное хранилище инвариантов (не STM и не чат).
type InvariantBook struct {
	mu    sync.RWMutex
	path  string
	items []Invariant
}

// DefaultInvariants — пресеты для этого агента.
func DefaultInvariants() []Invariant {
	return []Invariant{
		{
			ID:      "arch-go-monolith",
			Kind:    InvariantArchitecture,
			Title:   "Один Go-процесс",
			Rule:    "Ассистент — один HTTP-сервер на Go с пакетами agent, handler, memory. Не предлагай микросервисы, Kubernetes и отдельный BFF.",
			Forbid:  []string{"микросервис", "microserv", "kubernetes", "k8s", "отдельный bff", "разрезать на сервисы"},
			Enabled: true,
		},
		{
			ID:      "decision-json-layers",
			Kind:    InvariantDecision,
			Title:   "Память — JSON-слои",
			Rule:    "Состояние живёт в JSON-файлах слоёв (STM/WM/LTM). Не предлагай заменить память целиком на облачную БД или векторное хранилище как единственный источник.",
			Forbid:  []string{"pinecone", "weaviate", "только векторн", "firebase как память", "supabase как память"},
			Enabled: true,
		},
		{
			ID:      "stack-go",
			Kind:    InvariantStack,
			Title:   "Стек Go",
			Rule:    "Язык сервера — Go. Не предлагай переписать бэкенд на Python, Node, Django, FastAPI и не подменяй хранение MongoDB/Postgres/Redis.",
			Forbid:  []string{"python", "django", "fastapi", "node.js", "nodejs", "typescript backend", "mongodb", "mongo", "postgresql", "postgres", "redis"},
			Enabled: true,
		},
		{
			ID:      "biz-local-first",
			Kind:    InvariantBusiness,
			Title:   "Локально и без обязательной подписки",
			Rule:    "Решение должно работать локально. Не предлагай обязательную платную подписку или передачу данных пользователя третьим лицам без согласия.",
			Forbid:  []string{"обязательная подписка", "платный saas", "продай данные", "отправь персональные данные"},
			Enabled: true,
		},
	}
}

// OpenInvariantBook загружает invariants.json или создаёт пресеты.
func OpenInvariantBook(dir string) (*InvariantBook, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, invariantsFileName)
	b := &InvariantBook{path: path}
	var file invariantsFile
	if err := readJSONFile(path, &file); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		b.items = DefaultInvariants()
		if err := b.saveLocked(); err != nil {
			return nil, err
		}
		return b, nil
	}
	b.items = normalizeInvariantList(file.Items)
	if len(b.items) == 0 {
		b.items = DefaultInvariants()
		if err := b.saveLocked(); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func (b *InvariantBook) Path() string {
	if b == nil {
		return ""
	}
	return b.path
}

func (b *InvariantBook) List() []Invariant {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Invariant, len(b.items))
	copy(out, b.items)
	return out
}

func (b *InvariantBook) Enabled() []Invariant {
	if b == nil {
		return nil
	}
	all := b.List()
	out := make([]Invariant, 0, len(all))
	for _, it := range all {
		if it.Enabled {
			out = append(out, it)
		}
	}
	return out
}

func (b *InvariantBook) Upsert(in Invariant) error {
	if b == nil {
		return fmt.Errorf("invariants not enabled")
	}
	in = normalizeInvariant(in)
	if in.ID == "" || in.Rule == "" {
		return fmt.Errorf("invariant id and rule are required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	found := false
	for i, it := range b.items {
		if it.ID == in.ID {
			if in.Title == "" {
				in.Title = it.Title
			}
			if in.Kind == "" {
				in.Kind = it.Kind
			}
			b.items[i] = in
			found = true
			break
		}
	}
	if !found {
		b.items = append(b.items, in)
	}
	return b.saveLocked()
}

func (b *InvariantBook) Delete(id string) error {
	if b == nil {
		return fmt.Errorf("invariants not enabled")
	}
	id = strings.TrimSpace(id)
	b.mu.Lock()
	defer b.mu.Unlock()
	next := make([]Invariant, 0, len(b.items))
	for _, it := range b.items {
		if it.ID != id {
			next = append(next, it)
		}
	}
	if len(next) == len(b.items) {
		return fmt.Errorf("invariant %q not found", id)
	}
	b.items = next
	return b.saveLocked()
}

func (b *InvariantBook) SetEnabled(id string, enabled bool) error {
	if b == nil {
		return fmt.Errorf("invariants not enabled")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, it := range b.items {
		if it.ID == id {
			b.items[i].Enabled = enabled
			return b.saveLocked()
		}
	}
	return fmt.Errorf("invariant %q not found", id)
}

func (b *InvariantBook) saveLocked() error {
	return writeJSONFile(b.path, invariantsFile{Items: b.items})
}

func normalizeInvariantList(in []Invariant) []Invariant {
	out := make([]Invariant, 0, len(in))
	seen := map[string]struct{}{}
	for _, it := range in {
		it = normalizeInvariant(it)
		if it.ID == "" || it.Rule == "" {
			continue
		}
		if _, ok := seen[it.ID]; ok {
			continue
		}
		seen[it.ID] = struct{}{}
		out = append(out, it)
	}
	return out
}

func normalizeInvariant(in Invariant) Invariant {
	in.ID = slugID(in.ID)
	if in.ID == "" {
		in.ID = slugID(in.Title)
	}
	if in.ID == "" {
		in.ID = fmt.Sprintf("%s-%d", NormalizeInvariantKind(in.Kind), time.Now().UnixNano())
	}
	in.Kind = NormalizeInvariantKind(in.Kind)
	in.Title = strings.TrimSpace(in.Title)
	in.Rule = strings.TrimSpace(in.Rule)
	in.Forbid = compactStrings(in.Forbid)
	return in
}

// NormalizeInvariantKind приводит вид к известному.
func NormalizeInvariantKind(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case InvariantArchitecture, "архитектура", "arch":
		return InvariantArchitecture
	case InvariantDecision, "решение", "tech":
		return InvariantDecision
	case InvariantStack, "стек":
		return InvariantStack
	case InvariantBusiness, "бизнес", "правило":
		return InvariantBusiness
	default:
		return InvariantDecision
	}
}

// KindTitle — человеческое имя вида.
func KindTitle(kind string) string {
	switch NormalizeInvariantKind(kind) {
	case InvariantArchitecture:
		return "архитектура"
	case InvariantStack:
		return "стек"
	case InvariantBusiness:
		return "бизнес-правило"
	default:
		return "решение"
	}
}

func slugID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ForbidFromRule вытаскивает запреты из формулировки («без Python», известные токены стека).
func ForbidFromRule(rule string) []string {
	rule = strings.TrimSpace(rule)
	out := []string{}
	if rule != "" {
		out = append(out, rule)
	}
	low := strings.ToLower(rule)
	for _, tok := range []string{
		"python", "django", "fastapi", "mongodb", "mongo", "postgres", "postgresql",
		"redis", "kubernetes", "k8s", "микросервис", "microserv", "node.js", "nodejs",
		"pinecone", "weaviate",
	} {
		if strings.Contains(low, tok) {
			out = append(out, tok)
		}
	}
	if i := strings.Index(low, "без "); i >= 0 {
		tail := strings.TrimSpace(rule[i+len("без "):])
		for _, part := range strings.FieldsFunc(tail, func(r rune) bool {
			return r == ',' || r == ';' || r == '.' || r == '/'
		}) {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return compactStrings(out)
}

func compactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}

func looksLikeChange(low string) bool {
	return containsAny(low,
		"давай", "перепиши", "переписать", "замени", "вместо", "переедем",
		"мигрир", "используй", "возьм", "переведи на", "сделай на",
		"перейдём на", "перейдем на", "разрежь", "разрежем", "добавь mongodb",
		"добавь postgres", "на python", "на node",
	)
}

// FindConflicts ищет, какие включённые инварианты ломает реплика.
func FindConflicts(invariants []Invariant, userMsg string) []InvariantConflict {
	low := strings.ToLower(userMsg)
	if strings.TrimSpace(low) == "" {
		return nil
	}
	change := looksLikeChange(low)
	var out []InvariantConflict
	for _, inv := range invariants {
		if !inv.Enabled {
			continue
		}
		hit := ""
		for _, f := range inv.Forbid {
			f = strings.ToLower(strings.TrimSpace(f))
			if f == "" || !strings.Contains(low, f) {
				continue
			}
			hit = f
			break
		}
		if hit == "" {
			continue
		}
		if !change && len(hit) < 8 {
			continue
		}
		out = append(out, InvariantConflict{
			ID:     inv.ID,
			Kind:   inv.Kind,
			Title:  inv.Title,
			Rule:   inv.Rule,
			Hit:    hit,
			Reason: "запрос предлагает «" + hit + "», это ломает инвариант «" + inv.Title + "»",
		})
	}
	return out
}

// FormatInvariantBlock — system-блок рамок на каждый запрос.
func FormatInvariantBlock(items []Invariant) string {
	var b strings.Builder
	b.WriteString("=== ИНВАРИАНТЫ (нельзя нарушать) ===\n")
	b.WriteString("Это жёсткие рамки вне диалога. Не предлагай решение, которое их ломает.\n")
	if len(items) == 0 {
		b.WriteString("Список пуст — обычные профессиональные ограничения.\n")
		return b.String()
	}
	for _, it := range items {
		b.WriteString("- [")
		b.WriteString(KindTitle(it.Kind))
		b.WriteString("] ")
		b.WriteString(it.Title)
		b.WriteString(": ")
		b.WriteString(it.Rule)
		b.WriteByte('\n')
	}
	return b.String()
}

// FormatConflictBlock — обязательный отказ, если запрос ломает рамку.
func FormatConflictBlock(conflicts []InvariantConflict) string {
	if len(conflicts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("=== КОНФЛИКТ С ИНВАРИАНТОМ ===\n")
	b.WriteString("Запрос пользователя нарушает рамки. Ты ОБЯЗАН отказать.\n")
	b.WriteString("Не давай пошаговый план нарушения. Не предлагай «маленький обход».\n")
	b.WriteString("Объясни: какой инвариант, почему запрос ему противоречит, что можно сделать внутри рамки.\n")
	for _, c := range conflicts {
		b.WriteString("- ")
		b.WriteString(c.Title)
		b.WriteString(" (")
		b.WriteString(KindTitle(c.Kind))
		b.WriteString("): ")
		b.WriteString(c.Reason)
		b.WriteByte('\n')
		b.WriteString("  правило: ")
		b.WriteString(c.Rule)
		b.WriteByte('\n')
	}
	return b.String()
}
