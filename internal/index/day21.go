package index

// Hit — ближайший чанк по запросу, без вектора.
type Hit struct {
	Score   float64 `json:"score"`
	ChunkID string  `json:"chunk_id"`
	Section string  `json:"section"`
	Preview string  `json:"preview"`
}

// Day21 — снимок проверки индекса Terrano для чата.
type Day21 struct {
	Title              string       `json:"title"`
	Source             string       `json:"source"`
	Pages              int          `json:"pages"`
	Runes              int          `json:"runes"`
	Model              string       `json:"model"`
	Dim                int          `json:"dim"`
	Query              string       `json:"query"`
	Fixed              StrategyStat `json:"fixed"`
	Struct             StrategyStat `json:"struct"`
	HeadingsSplitFixed int          `json:"headings_split_by_fixed"`
	Sections           []string     `json:"sections"`
	FixedHits          []Hit        `json:"fixed_hits"`
	StructHits         []Hit        `json:"struct_hits"`
}

// Snapshot возвращает результат прогона на локальном сервере.
func Snapshot() Day21 {
	return Day21{
		Title:  "День 21. Индекс Terrano",
		Source: "Manual_Terrano.pdf",
		Pages:  264,
		Runes:  381799,
		Model:  "nomic-embed-text",
		Dim:    768,
		Query:  "какое давление должно быть в шинах",
		Fixed: StrategyStat{
			Chunks: 449, AvgRunes: 999, MinRunes: 997, MaxRunes: 1000, Sections: 8,
		},
		Struct: StrategyStat{
			Chunks: 248, AvgRunes: 1536, MinRunes: 63, MaxRunes: 1800, Sections: 8,
		},
		HeadingsSplitFixed: 8,
		Sections: []string{
			"начало",
			"Глава 1: Знакомство с автомобилем",
			"Глава 2: Вождение автомобиля",
			"Глава 3: Комфорт",
			"Глава 4: Техническое обслуживание",
			"Глава 5: Практические советы",
			"Глава 6: Технические характеристики",
			"Глава 7. Аудиосистема",
		},
		FixedHits: []Hit{
			{Score: 0.788, ChunkID: "fixed:manual-terrano:314", Section: "Глава 5: Практические советы", Preview: "ть давление в холодных шинах, следует увеличить рекомендованные значения на 0,2 -0,3 бар."},
			{Score: 0.770, ChunkID: "fixed:manual-terrano:317", Section: "Глава 5: Практические советы", Preview: "е сцепление с дорогой. Внимание! Иногда на этих шинах указаны направление вращения колеса."},
			{Score: 0.753, ChunkID: "fixed:manual-terrano:315", Section: "Глава 5: Практические советы", Preview: "от условий эксплуатации (см. раздел «Давление воздуха в шинах» в главе 4)."},
		},
		StructHits: []Hit{
			{Score: 0.799, ChunkID: "struct:manual-terrano:171", Section: "Глава 5: Практические советы", Preview: "Шины - единственный элемент автомобиля, который находится в постоянном контакте с дорогой, поэтому очень важно"},
			{Score: 0.762, ChunkID: "struct:manual-terrano:174", Section: "Глава 5: Практические советы", Preview: "ШИНЫ (4/4) Шины для движения по льду и снегу, или «зимние» шины"},
			{Score: 0.757, ChunkID: "struct:manual-terrano:156", Section: "Глава 4: Техническое обслуживание", Preview: "B: размеры шин, установленных на автомобиле. C: давление воздуха в шинах передних колес при езде вне автострад"},
		},
	}
}
