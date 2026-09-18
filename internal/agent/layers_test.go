package agent

import (
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/memory"
)

func TestExplicitPrefixRouting(t *testing.T) {
	cases := []struct {
		msg      string
		wantWM   bool
		wantLTM  bool
		wantName string
	}{
		{msg: "#задача ТЗ для FoodDash", wantWM: true},
		{msg: "#профиль Тоша", wantLTM: true, wantName: "Тоша"},
		{msg: "#решение stack = Go+Flutter", wantLTM: true},
		{msg: "#знание контакт: Анна", wantLTM: true},
		{msg: "#запомни люблю списки", wantLTM: true},
	}
	for _, tc := range cases {
		d, src := parseExplicitRoute(tc.msg)
		if src != "prefix" {
			t.Fatalf("%q: source=%s", tc.msg, src)
		}
		if tc.wantWM && (d.Working == nil || !workingPatchUseful(*d.Working)) {
			t.Fatalf("%q: expected WM patch", tc.msg)
		}
		if tc.wantLTM && (d.LongTerm == nil || !longTermPatchUseful(*d.LongTerm)) {
			t.Fatalf("%q: expected LTM patch", tc.msg)
		}
		if tc.wantName != "" && d.LongTerm.ProfileName != tc.wantName {
			t.Fatalf("%q: name=%q", tc.msg, d.LongTerm.ProfileName)
		}
	}
}

func TestHeuristicRouteSplitsLayers(t *testing.T) {
	d, src := heuristicRoute("Меня зовут Тоша, предпочитаю краткие ответы.")
	if src != "heuristic" {
		t.Fatalf("source %s", src)
	}
	if d.LongTerm == nil || d.LongTerm.ProfileName != "Тоша" {
		t.Fatalf("profile: %+v", d.LongTerm)
	}
	if d.Working != nil {
		t.Fatalf("profile must not go to WM")
	}

	d, _ = heuristicRoute("Задача: собрать ТЗ, бюджет 2 млн.")
	if d.Working == nil || d.Working.Goal == "" {
		t.Fatalf("task should go to WM: %+v", d.Working)
	}

	intro := "Привет Я Антон занимаюсь умным домом и ИИ\nхочу сделать супер умным свой дом"
	d, _ = heuristicRoute(intro)
	if d.LongTerm == nil || d.LongTerm.ProfileName != "Антон" {
		t.Fatalf("intro name: %+v", d.LongTerm)
	}
	if d.Working == nil || d.Working.Goal == "" {
		t.Fatalf("intro goal should go to WM: %+v", d.Working)
	}
	if len(d.LongTerm.Knowledge) == 0 {
		t.Fatalf("intro occupation should go to LTM: %+v", d.LongTerm)
	}
}

func TestPromptUsesSeparateLayers(t *testing.T) {
	dir := t.TempDir()
	layers, err := memory.OpenLayers(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = layers.ApplyLongTerm(memory.LongTermPatch{ProfileName: "Тоша", Knowledge: []memory.KnowledgeItem{{Topic: "pet", Fact: "синий попугай"}}})
	_ = layers.ApplyWorking(memory.WorkingPatch{Goal: "FoodDash ТЗ", Status: memory.WorkingActive, Constraints: []string{"бюджет 2 млн"}})
	_, _ = layers.AppendShortTerm(8,
		deepseek.Message{Role: "user", Content: "старый маленький разговор"},
		deepseek.Message{Role: "assistant", Content: "ок"},
	)

	a := New("test").WithLayers(layers).WithMemoryPolicy(MemoryPolicy{STMWindowN: 8, InjectSTM: true, InjectWM: true, InjectLTM: true})
	msgs, info := a.buildMessagesLayers("как меня зовут?", a.MemoryPolicy())

	blob := ""
	for _, m := range msgs {
		blob += m.Role + ":" + m.Content + "\n"
	}
	if !strings.Contains(blob, "ДОЛГОВРЕМЕННАЯ") || !strings.Contains(blob, "Тоша") {
		t.Fatalf("LTM missing in prompt:\n%s", blob)
	}
	if !strings.Contains(blob, "РАБОЧАЯ") || !strings.Contains(blob, "FoodDash") {
		t.Fatalf("WM missing in prompt:\n%s", blob)
	}
	if !strings.Contains(blob, "старый маленький разговор") {
		t.Fatalf("STM missing in prompt:\n%s", blob)
	}
	if !info.WorkingActive || info.LongTerm.Profile.Name != "Тоша" {
		t.Fatalf("snapshot: %+v", info)
	}

	a.SetMemoryPolicy(MemoryPolicy{STMWindowN: 8, InjectSTM: false, InjectWM: false, InjectLTM: true})
	msgs, _ = a.buildMessagesLayers("как меня зовут?", a.MemoryPolicy())
	blob = ""
	for _, m := range msgs {
		blob += m.Content + "\n"
	}
	if !strings.Contains(blob, "Тоша") {
		t.Fatalf("LTM-only should still have name")
	}
	if strings.Contains(blob, "FoodDash") {
		t.Fatalf("WM should be excluded")
	}
	if strings.Contains(blob, "старый маленький разговор") {
		t.Fatalf("STM should be excluded")
	}
}

func TestMemoryRouterPromptContainsLayersAndUtterance(t *testing.T) {
	p := buildMemoryRouterPrompt(
		"Я Антон, хочу сделать умный дом",
		memory.WorkingState{Goal: "уже есть задача", Status: memory.WorkingActive},
		memory.LongTermState{Profile: memory.Profile{Name: "Тоша"}},
	)
	if !strings.Contains(p, "Разложи реплику") {
		t.Fatalf("missing router instruction:\n%s", p)
	}
	if !strings.Contains(p, "Я Антон, хочу сделать умный дом") {
		t.Fatalf("missing user utterance:\n%s", p)
	}
	if !strings.Contains(p, "уже есть задача") || !strings.Contains(p, "Тоша") {
		t.Fatalf("missing current layer snapshots:\n%s", p)
	}
}

func TestClearLayerIsolation(t *testing.T) {
	dir := t.TempDir()
	layers, err := memory.OpenLayers(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = layers.AppendShortTerm(8, deepseek.Message{Role: "user", Content: "hi"}, deepseek.Message{Role: "assistant", Content: "yo"})
	_ = layers.ApplyWorking(memory.WorkingPatch{Goal: "g"})
	_ = layers.ApplyLongTerm(memory.LongTermPatch{ProfileName: "Тоша"})

	a := New("test").WithLayers(layers)
	if err := a.ClearLayer(memory.LayerShortTerm); err != nil {
		t.Fatal(err)
	}
	if a.Layers().ShortTermLen() != 0 || a.Layers().Working().Goal != "g" || a.Layers().LongTerm().Profile.Name != "Тоша" {
		t.Fatalf("STM clear leaked")
	}
}

func TestPromptInjectsActiveProfile(t *testing.T) {
	dir := t.TempDir()
	layers, err := memory.OpenLayers(dir)
	if err != nil {
		t.Fatal(err)
	}
	book, err := memory.OpenProfileBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = book.Activate("exec")
	a := New("test").WithLayers(layers).WithProfiles(book).WithMemoryPolicy(MemoryPolicy{
		STMWindowN: 4, InjectSTM: false, InjectWM: false, InjectLTM: false, InjectProfile: true,
	})
	msgs, info := a.buildMessagesLayers("что делать?", a.MemoryPolicy())
	blob := ""
	for _, m := range msgs {
		blob += m.Content + "\n"
	}
	if !strings.Contains(blob, "ПРОФИЛЬ ПОЛЬЗОВАТЕЛЯ") || !strings.Contains(blob, "Ирина") {
		t.Fatalf("profile missing:\n%s", blob)
	}
	if strings.Contains(blob, "=== ДОЛГОВРЕМЕННАЯ") {
		t.Fatalf("LTM should be off")
	}
	if !info.ProfileInPrompt || info.Profile.Name != "Ирина" {
		t.Fatalf("snapshot: %+v", info.Profile)
	}

	a.SetMemoryPolicy(MemoryPolicy{STMWindowN: 4, InjectProfile: false})
	msgs, info = a.buildMessagesLayers("что делать?", a.MemoryPolicy())
	blob = ""
	for _, m := range msgs {
		blob += m.Content + "\n"
	}
	if strings.Contains(blob, "ПРОФИЛЬ ПОЛЬЗОВАТЕЛЯ") {
		t.Fatalf("profile should be excluded")
	}
	if info.ProfileInPrompt {
		t.Fatal("profile_in_prompt")
	}
}

func TestExplicitStylePrefixUpdatesProfile(t *testing.T) {
	d, src := parseExplicitRoute("#стиль коротко списком")
	if src != "prefix" || d.Profile == nil || d.Profile.Style != "коротко списком" {
		t.Fatalf("%s %+v", src, d.Profile)
	}
}

func TestTaskPrefixesPauseResumeAdvance(t *testing.T) {
	d, src := parseExplicitRoute("#пауза")
	if src != "prefix" || d.Working == nil || d.Working.Event != memory.TaskPause {
		t.Fatalf("pause: %s %+v", src, d.Working)
	}
	d, src = parseExplicitRoute("#продолжи")
	if src != "prefix" || d.Working == nil || d.Working.Event != memory.TaskResume {
		t.Fatalf("resume: %s %+v", src, d.Working)
	}
	d, src = parseExplicitRoute("#этап validation")
	if src != "prefix" || d.Working == nil || memory.NormalizeStage(d.Working.Stage) != memory.StageValidation {
		t.Fatalf("stage: %s %+v", src, d.Working)
	}
	d, src = parseExplicitRoute("#шаг собрать стены")
	if src != "prefix" || d.Working == nil || d.Working.Step != "собрать стены" {
		t.Fatalf("step: %s %+v", src, d.Working)
	}
	d, src = parseExplicitRoute("#ожидаю подтверждение материала")
	if src != "prefix" || d.Working == nil || d.Working.Expect == "" {
		t.Fatalf("expect: %s %+v", src, d.Working)
	}
}

func TestPromptInjectsTaskMachine(t *testing.T) {
	dir := t.TempDir()
	layers, err := memory.OpenLayers(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = layers.ApplyWorking(memory.WorkingPatch{
		Goal: "дом", Event: memory.TaskSet, Stage: memory.StageExecution,
		Step: "стены", Expect: "материал", AddDone: []string{"фундамент готов"},
	})
	_ = layers.ApplyTask(memory.TaskPause, memory.WorkingPatch{})

	a := New("test").WithLayers(layers).WithMemoryPolicy(MemoryPolicy{STMWindowN: 4, InjectSTM: false, InjectWM: true, InjectLTM: false})
	msgs, info := a.buildMessagesLayers("напомни план", a.MemoryPolicy())
	blob := ""
	for _, m := range msgs {
		blob += m.Content + "\n"
	}
	if !strings.Contains(blob, "СОСТОЯНИЕ ЗАДАЧИ") || !strings.Contains(blob, "ВЫПОЛНЕНИЕ") {
		t.Fatalf("fsm missing:\n%s", blob)
	}
	if !strings.Contains(blob, "ПАУЗА") || !strings.Contains(blob, "стены") {
		t.Fatalf("pause/step missing:\n%s", blob)
	}
	if !strings.Contains(blob, "фундамент готов") {
		t.Fatalf("done_so_far missing:\n%s", blob)
	}
	if !info.WorkingActive {
		t.Fatal("paused execution still active")
	}

	_ = layers.ApplyTask(memory.TaskResume, memory.WorkingPatch{})
	msgs, _ = a.buildMessagesLayers("продолжай", a.MemoryPolicy())
	blob = ""
	for _, m := range msgs {
		blob += m.Content + "\n"
	}
	if strings.Contains(blob, "ПАУЗА на этом этапе") {
		t.Fatalf("resume still paused:\n%s", blob)
	}
	if !strings.Contains(blob, "стены") {
		t.Fatalf("resume lost step:\n%s", blob)
	}
}
