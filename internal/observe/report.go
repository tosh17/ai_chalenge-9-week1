package observe

import (
	"fmt"
	"strings"
)

// ReportText — подробный ряд замеров без выводов. Динамику и заключение пишет модель.
// В шапке — фактическое время первого и последнего замера, не окно запроса.
func ReportText(city string, rows []Sample) string {
	var b strings.Builder
	tz := ""
	span := "записей нет"
	if len(rows) > 0 {
		tz = rows[0].Timezone
		span = civilLabel(rows[0].ObservedAt) + " — " + civilLabel(rows[len(rows)-1].ObservedAt)
	}
	header := fmt.Sprintf("Циклов сбора: %d. %s, %s", len(rows), city, span)
	if tz != "" {
		header += ", время местное (" + tz + ")"
	}
	b.WriteString(header)
	b.WriteByte('\n')
	if len(rows) == 0 {
		b.WriteString("Записей нет. Сбор идёт каждые 15 минут, файл появится после первого замера.\n")
		return b.String()
	}
	b.WriteString("Ряд замеров, без выводов. Каждая строка — один цикл сбора.\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- %s, %.1f °C, %s%s%s, восход %s, закат %s, луна %s %.2f, сохранено %s\n",
			row.ObservedAt,
			row.TemperatureC,
			dash(row.Weather),
			optionalPct("влажность", row.Humidity),
			optionalUnit("ветер", row.WindKmh, "км/ч"),
			dash(row.Sunrise),
			dash(row.Sunset),
			dash(row.MoonName),
			row.MoonPhase,
			dash(row.SavedAt),
		)
	}
	return b.String()
}

func civilLabel(value string) string {
	value = strings.TrimSpace(strings.Replace(value, "T", " ", 1))
	if len(value) >= 16 {
		return value[:16]
	}
	return dash(value)
}

func optionalPct(label string, v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf(", %s %.0f%%", label, *v)
}

func optionalUnit(label string, v *float64, unit string) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf(", %s %.1f %s", label, *v, unit)
}

func dash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}
