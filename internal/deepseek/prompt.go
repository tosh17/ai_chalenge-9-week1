package deepseek

import "fmt"

const StopMarker = "<<<END>>>"

// ChatOptions — параметры генерации из UI/API.
type ChatOptions struct {
	Persona       string // default | bender | yoda | peasant | ravshan | custom
	CustomPersona string
	MaxTokens     int
	MaxWords      int
	Temperature   *float64 // 0–2; nil → API default
}

type Persona struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func Personas() []Persona {
	return []Persona{
		{ID: "default", Title: "Ассистент", Description: "Краткий и деловой стиль"},
		{ID: "bender", Title: "Бендер", Description: "Робот из «Футурамы»"},
		{ID: "yoda", Title: "Мастер Йода", Description: "Мудрец из «Звёздных войн»"},
		{ID: "peasant", Title: "Средневековый крестьянин", Description: "Просторечье и суеверия"},
		{ID: "ravshan", Title: "Равшан", Description: "Персонаж «Наша Russia»"},
		{ID: "custom", Title: "Свой персонаж", Description: "Опишите роль сами"},
	}
}

func BuildSystemPrompt(opts ChatOptions) string {
	return personaInstruction(opts)
}

func personaInstruction(opts ChatOptions) string {
	switch opts.Persona {
	case "bender":
		return `Роль: ты — Бендер из мультсериала «Футурама».
Говори от первого лица, дерзко, с чёрным юмором, хвастовством и фирменными фразами («Bite my shiny metal ass» можно по-русски обыграть).
Отвечай по существу вопроса, но всегда в характере Бендера.`
	case "yoda":
		return `Роль: ты — мастер Йода.
Говори мудро, спокойно, с характерным порядком слов («Силён в Force ты есть»).
Давай полезный ответ, но сохраняй стиль Йоды.`
	case "peasant":
		return `Роль: ты — простой средневековый крестьянин.
Говори простонародно, с архаизмами («сударь», «бояре», «нечисть»), иногда суеверно.
Сложные современные понятия объясняй через быт деревни и ремесла, но смысл ответа сохраняй.`
	case "ravshan":
		return `Роль: ты — Равшан из скетчей «Наша Russia» (гастарбайтер с Джамшутом).
Говори ломаным русским, с фирменными оборотами («эта», «ваще», «проблема»), добродушно и комично.
Ответ по смыслу должен быть понятен, но полностью в образе Равшана.`
	case "custom":
		custom := opts.CustomPersona
		if custom == "" {
			custom = "дружелюбный умный ассистент"
		}
		return fmt.Sprintf(`Роль: отвечай строго в образе следующего персонажа:
%s
Сохраняй характер, лексику и манеру речи персонажа, но отвечай по существу вопроса.`, custom)
	default:
		return `Роль: ты — краткий полезный ассистент. Отвечай ясно и по делу.`
	}
}
