package observe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Sample — одна строка архива наблюдений.
type Sample struct {
	City         string   `json:"city"`
	Country      string   `json:"country,omitempty"`
	Latitude     float64  `json:"latitude"`
	Longitude    float64  `json:"longitude"`
	Timezone     string   `json:"timezone"`
	ObservedAt   string   `json:"observed_at"`
	TemperatureC float64  `json:"temperature_c"`
	Weather      string   `json:"weather"`
	Humidity     *float64 `json:"humidity,omitempty"`
	WindKmh      *float64 `json:"wind_kmh,omitempty"`
	Sunrise      string   `json:"sunrise,omitempty"`
	Sunset       string   `json:"sunset,omitempty"`
	MoonPhase    float64  `json:"moon_phase"`
	MoonName     string   `json:"moon_name"`
	SavedAt      string   `json:"saved_at"`
}

// Store дописывает JSONL и собирает сводку за период.
type Store struct {
	dir string
	mu  sync.Mutex
}

func Open(dir string) *Store {
	if dir == "" {
		dir = filepath.Join("data", "observations")
	}
	return &Store{dir: dir}
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) path(city string) string {
	return filepath.Join(s.dir, fileName(city))
}

// Append дописывает замер. Если предыдущий моложе minGap, возвращает его и fresh=false.
func (s *Store) Append(sample Sample, minGap time.Duration) (saved Sample, fresh bool, err error) {
	if strings.TrimSpace(sample.City) == "" {
		return Sample{}, false, fmt.Errorf("city is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return Sample{}, false, err
	}
	path := s.path(sample.City)
	if last, ok := lastSample(path); ok && minGap > 0 {
		if at, err := time.Parse(time.RFC3339, last.SavedAt); err == nil && time.Since(at) < minGap {
			return last, false, nil
		}
	}
	sample.SavedAt = time.Now().UTC().Format(time.RFC3339)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Sample{}, false, err
	}
	defer f.Close()
	raw, err := json.Marshal(sample)
	if err != nil {
		return Sample{}, false, err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return Sample{}, false, err
	}
	return sample, true, nil
}

func (s *Store) Between(city string, from, to time.Time) ([]Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path(city))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Sample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var sample Sample
		if err := json.Unmarshal([]byte(line), &sample); err != nil {
			continue
		}
		at := sample.when()
		if at.IsZero() || at.Before(from) || at.After(to) {
			continue
		}
		out = append(out, sample)
	}
	return out, sc.Err()
}

func (s *Sample) when() time.Time {
	loc := time.Local
	if s.Timezone != "" {
		if l, err := time.LoadLocation(s.Timezone); err == nil {
			loc = l
		}
	}
	if t, err := parseInLocation(s.ObservedAt, loc); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s.SavedAt); err == nil {
		return t
	}
	return time.Time{}
}

func lastSample(path string) (Sample, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Sample{}, false
	}
	defer f.Close()
	var last Sample
	ok := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var sample Sample
		if json.Unmarshal([]byte(line), &sample) == nil {
			last = sample
			ok = true
		}
	}
	return last, ok
}

func fileName(city string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(city) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		name = "city"
	}
	return name + ".jsonl"
}

func parseInLocation(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if loc == nil {
		loc = time.UTC
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad time %q", value)
}
