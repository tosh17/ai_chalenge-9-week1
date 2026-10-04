package agent

import (
	"strings"
	"testing"
)

func TestMergeTaskKeepsGoal(t *testing.T) {
	prev := TaskView{Goal: "завтрак: какао", Constraints: []string{"без яиц"}}
	got := mergeTask(prev, taskUpdate{Clarified: []string{"порция на двоих"}, Constraints: []string{"без яиц", "молоко не заменять"}})
	if got.Goal != prev.Goal {
		t.Fatalf("цель сброшена: %+v", got)
	}
	if len(got.Constraints) != 2 || len(got.Clarified) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestParseTaskUpdateReadsJSON(t *testing.T) {
	upd, ok := parseTaskUpdate("вот\n{\"goal\":\"голландский соус\",\"clarified\":[],\"constraints\":[\"без кайена\"]}")
	if !ok || !strings.Contains(upd.Goal, "соус") || len(upd.Constraints) != 1 {
		t.Fatalf("%+v %v", upd, ok)
	}
}
