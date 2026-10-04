package index

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestControlSourcesAgainstIndex(t *testing.T) {
	if os.Getenv("COOKBOOK_EVAL") == "" {
		t.Skip("COOKBOOK_EVAL=1")
	}
	s, err := OpenSearcher("../../data/cookbook/struct.json", "http://10.10.1.101:11434", "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, c := range Controls() {
		vec, err := s.QueryVector(ctx, c.Search)
		if err != nil {
			t.Fatal(err)
		}
		hits := s.Matches(vec, 4)
		fmt.Printf("source=%v want=%q q=%s\n", SectionHit(hits, c.Section), c.Section, c.Search)
		for _, h := range hits {
			fmt.Printf("  %.3f %s\n", h.Score, h.Section)
		}
	}
}
