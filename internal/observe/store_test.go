package observe

import (
	"strings"
	"testing"
	"time"
)

func TestAppendAndReportListsSamples(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	hum, wind := 80.0, 3.6
	night := Sample{
		City: "Волгоград", ObservedAt: "2026-09-25T03:00", TemperatureC: 12,
		Sunrise: "2026-09-25T06:00", Sunset: "2026-09-25T18:00",
		MoonPhase: 0.40, MoonName: "растущая луна", Weather: "ясно", Timezone: "UTC",
		Humidity: &hum, WindKmh: &wind,
	}
	day := night
	day.ObservedAt = "2026-09-25T14:00"
	day.TemperatureC = 22
	if _, fresh, err := store.Append(night, 0); err != nil || !fresh {
		t.Fatal(err, fresh)
	}
	if _, fresh, err := store.Append(day, 0); err != nil || !fresh {
		t.Fatal(err, fresh)
	}
	rows, err := store.Between("Волгоград", time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	text := ReportText("Волгоград", rows)
	for _, want := range []string{"Циклов сбора: 2", "2026-09-25 03:00 — 2026-09-25 14:00", "2026-09-25T03:00", "12.0 °C", "2026-09-25T14:00", "22.0 °C", "восход 2026-09-25T06:00", "закат 2026-09-25T18:00", "влажность 80%", "ветер 3.6 км/ч", "растущая луна"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Средняя температура") || strings.Contains(text, "ночь") {
		t.Fatalf("report must not pre-analyze:\n%s", text)
	}
	if _, fresh, err := store.Append(day, time.Hour); err != nil || fresh {
		t.Fatal("fresh sample inside the gap should be skipped", err, fresh)
	}
}
