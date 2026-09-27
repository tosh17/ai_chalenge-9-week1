package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const profilesFileName = "profiles.json"

// UserProfile — персонализация ассистента поверх слоёв памяти.
// Стиль/формат/ограничения подмешиваются в каждый запрос, пока профиль активен.
type UserProfile struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Name        string   `json:"name,omitempty"`
	Role        string   `json:"role,omitempty"`
	Language    string   `json:"language,omitempty"`
	Style       string   `json:"style,omitempty"`
	Format      string   `json:"format,omitempty"`
	Constraints []string `json:"constraints,omitempty"`
}

// Empty — нет ни идентичности, ни предпочтений.
func (p UserProfile) Empty() bool {
	return strings.TrimSpace(p.Name) == "" &&
		strings.TrimSpace(p.Role) == "" &&
		strings.TrimSpace(p.Style) == "" &&
		strings.TrimSpace(p.Format) == "" &&
		len(p.Constraints) == 0
}

// UserProfilePatch — частичное обновление активного профиля.
type UserProfilePatch struct {
	Name           string   `json:"name,omitempty"`
	Role           string   `json:"role,omitempty"`
	Language       string   `json:"language,omitempty"`
	Style          string   `json:"style,omitempty"`
	Format         string   `json:"format,omitempty"`
	AddConstraints []string `json:"add_constraints,omitempty"`
}

func (p UserProfilePatch) Useful() bool {
	return p.Name != "" || p.Role != "" || p.Language != "" || p.Style != "" || p.Format != "" || len(p.AddConstraints) > 0
}

type profilesFile struct {
	ActiveID string                 `json:"active_id"`
	Profiles map[string]UserProfile `json:"profiles"`
}

// ProfileBook — набор именованных профилей и текущий активный.
type ProfileBook struct {
	mu       sync.RWMutex
	path     string
	activeID string
	profiles map[string]UserProfile
}

// DefaultProfiles — пресеты для сравнения персонализации.
func DefaultProfiles() []UserProfile {
	return []UserProfile{
		{
			ID:       "brief",
			Title:    "Бригадир · кратко",
			Name:     "Антон",
			Role:     "прораб, руководит двумя строительными бригадами",
			Language: "ru",
			Style:    "кратко, по делу, без воды",
			Format:   "маркированные списки и короткие пункты",
			Constraints: []string{
				"не больше 8 пунктов",
				"без вступлений и дисклеймеров",
				"термины стройки можно не расшифровывать",
			},
		},
		{
			ID:       "exec",
			Title:    "Заказчик · формально",
			Name:     "Ирина",
			Role:     "заказчик строительства, принимает решения по бюджету",
			Language: "ru",
			Style:    "строго формально, сначала вывод",
			Format:   "сначала 1–2 предложения вывода, затем нумерованные шаги",
			Constraints: []string{
				"без сленга и эмодзи",
				"обращайся на «вы»",
				"не давай окончательной рекомендации, пока не назван бюджет",
			},
		},
		{
			ID:       "observer",
			Title:    "Наблюдатель",
			Name:     "",
			Role:     "разбор архива сборщика наблюдений по Волгограду",
			Language: "ru",
			Style:    "кратко, сначала вывод",
			Format:   "сначала вывод с цифрами из инструмента, затем кратко по делу",
			Constraints: []string{
				"цифры бери только из ответа инструмента",
				"не выдумывай замеры и размеры файлов",
			},
		},
		{
			ID:       "learner",
			Title:    "Новичок · с примерами",
			Name:     "Саша",
			Role:     "впервые строит дом, мало опыта",
			Language: "ru",
			Style:    "дружелюбно и просто, как наставник",
			Format:   "короткие абзацы, каждый совет с бытовым примером",
			Constraints: []string{
				"без жаргона без расшифровки",
				"не больше 6 предложений",
				"не пугай сложностями",
			},
		},
	}
}

// OpenProfileBook загружает profiles.json или создаёт пресеты.
func OpenProfileBook(dir string) (*ProfileBook, error) {
	if dir == "" {
		return nil, fmt.Errorf("profile book: dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("profile book: mkdir: %w", err)
	}
	b := &ProfileBook{
		path:     filepath.Join(dir, profilesFileName),
		profiles: map[string]UserProfile{},
	}
	data, err := os.ReadFile(b.path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("profile book: read: %w", err)
		}
		for _, p := range DefaultProfiles() {
			b.profiles[p.ID] = cloneProfile(p)
		}
		b.activeID = "brief"
		if err := b.saveLocked(); err != nil {
			return nil, err
		}
		return b, nil
	}
	var payload profilesFile
	if len(data) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("profile book: parse: %w", err)
		}
	}
	if payload.Profiles == nil {
		payload.Profiles = map[string]UserProfile{}
	}
	for id, p := range payload.Profiles {
		if p.ID == "" {
			p.ID = id
		}
		b.profiles[id] = cloneProfile(p)
	}
	if len(b.profiles) == 0 {
		for _, p := range DefaultProfiles() {
			b.profiles[p.ID] = cloneProfile(p)
		}
	}
	b.activeID = payload.ActiveID
	if _, ok := b.profiles[b.activeID]; !ok {
		for _, p := range DefaultProfiles() {
			if _, exists := b.profiles[p.ID]; exists {
				b.activeID = p.ID
				break
			}
		}
		if _, ok := b.profiles[b.activeID]; !ok {
			for id := range b.profiles {
				b.activeID = id
				break
			}
		}
	}
	return b, nil
}

// Path — JSON-файл книги профилей.
func (b *ProfileBook) Path() string {
	if b == nil {
		return ""
	}
	return b.path
}

// ActiveID — id текущего профиля.
func (b *ProfileBook) ActiveID() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.activeID
}

// Active — копия активного профиля.
func (b *ProfileBook) Active() UserProfile {
	if b == nil {
		return UserProfile{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return cloneProfile(b.profiles[b.activeID])
}

// List — все профили (для UI).
func (b *ProfileBook) List() []UserProfile {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]UserProfile, 0, len(b.profiles))
	for _, id := range []string{"brief", "exec", "learner"} {
		if p, ok := b.profiles[id]; ok {
			out = append(out, cloneProfile(p))
		}
	}
	for id, p := range b.profiles {
		if id == "brief" || id == "exec" || id == "learner" {
			continue
		}
		out = append(out, cloneProfile(p))
	}
	return out
}

// Activate переключает активный профиль.
func (b *ProfileBook) Activate(id string) error {
	if b == nil {
		return fmt.Errorf("profile book: nil")
	}
	id = strings.TrimSpace(id)
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.profiles[id]; !ok {
		return fmt.Errorf("unknown profile %q", id)
	}
	b.activeID = id
	return b.saveLocked()
}

// Upsert сохраняет профиль (новый id или правка существующего).
func (b *ProfileBook) Upsert(p UserProfile) error {
	if b == nil {
		return fmt.Errorf("profile book: nil")
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return fmt.Errorf("profile id is required")
	}
	p.Constraints = cleanStrings(p.Constraints)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.profiles[p.ID] = cloneProfile(p)
	if b.activeID == "" {
		b.activeID = p.ID
	}
	return b.saveLocked()
}

// PatchActive мержит поля в текущий профиль.
func (b *ProfileBook) PatchActive(patch UserProfilePatch) error {
	if b == nil || !patch.Useful() {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	cur := b.profiles[b.activeID]
	if cur.ID == "" {
		cur.ID = b.activeID
		if cur.ID == "" {
			cur.ID = "custom"
			b.activeID = cur.ID
		}
	}
	if patch.Name != "" {
		cur.Name = patch.Name
	}
	if patch.Role != "" {
		cur.Role = patch.Role
	}
	if patch.Language != "" {
		cur.Language = patch.Language
	}
	if patch.Style != "" {
		cur.Style = patch.Style
	}
	if patch.Format != "" {
		cur.Format = patch.Format
	}
	cur.Constraints = appendUnique(cur.Constraints, patch.AddConstraints...)
	b.profiles[cur.ID] = cur
	return b.saveLocked()
}

func (b *ProfileBook) saveLocked() error {
	payload := profilesFile{ActiveID: b.activeID, Profiles: b.profiles}
	return writeJSONFile(b.path, payload)
}

func cloneProfile(in UserProfile) UserProfile {
	out := in
	if in.Constraints != nil {
		out.Constraints = append([]string{}, in.Constraints...)
	}
	return out
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// FormatProfileBlock — system-блок, который уходит в каждый запрос.
func FormatProfileBlock(p UserProfile) string {
	var b strings.Builder
	b.WriteString("=== ПРОФИЛЬ ПОЛЬЗОВАТЕЛЯ (персонализация) ===\n")
	if p.Empty() {
		b.WriteString("Профиль не задан. Отвечай нейтрально, без выдуманных предпочтений.\n")
		return b.String()
	}
	b.WriteString("Соблюдай стиль, формат и ограничения автоматически, без фраз «как вы просили».\n")
	if p.Name != "" {
		b.WriteString("Имя: ")
		b.WriteString(p.Name)
		b.WriteByte('\n')
	}
	if p.Role != "" {
		b.WriteString("Роль: ")
		b.WriteString(p.Role)
		b.WriteByte('\n')
	}
	if p.Language != "" {
		b.WriteString("Язык: ")
		b.WriteString(p.Language)
		b.WriteByte('\n')
	}
	if p.Style != "" {
		b.WriteString("Стиль ответа: ")
		b.WriteString(p.Style)
		b.WriteByte('\n')
	}
	if p.Format != "" {
		b.WriteString("Формат: ")
		b.WriteString(p.Format)
		b.WriteByte('\n')
	}
	for _, c := range p.Constraints {
		b.WriteString("- ограничение: ")
		b.WriteString(c)
		b.WriteByte('\n')
	}
	return b.String()
}
