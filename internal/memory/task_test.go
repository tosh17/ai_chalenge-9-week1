package memory_test

import (
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/memory"
)

func TestTaskMachinePauseAtAnyStageAndResume(t *testing.T) {
	stages := []string{memory.StagePlanning, memory.StageExecution, memory.StageValidation}
	for _, st := range stages {
		cur := memory.TaskState{Stage: st, Step: "шаг " + st, Expect: "ждать"}
		paused, err := memory.ApplyTaskEvent(cur, memory.TaskPause, memory.WorkingPatch{})
		if err != nil {
			t.Fatalf("%s pause: %v", st, err)
		}
		if !paused.Paused || paused.Stage != st {
			t.Fatalf("%s: pause must keep stage, got %+v", st, paused)
		}
		resumed, err := memory.ApplyTaskEvent(paused, memory.TaskResume, memory.WorkingPatch{})
		if err != nil {
			t.Fatalf("%s resume: %v", st, err)
		}
		if resumed.Paused || resumed.Stage != st || resumed.Step != "шаг "+st {
			t.Fatalf("%s: resume must keep step, got %+v", st, resumed)
		}
	}
}

func TestTaskMachineAdvanceOrder(t *testing.T) {
	t0 := memory.TaskState{Stage: memory.StageIdle}
	cur, err := memory.ApplyTaskEvent(t0, memory.TaskStart, memory.WorkingPatch{})
	if err != nil || cur.Stage != memory.StagePlanning {
		t.Fatalf("start: %+v %v", cur, err)
	}
	for _, want := range []string{memory.StageExecution, memory.StageValidation, memory.StageDone} {
		cur, err = memory.ApplyTaskEvent(cur, memory.TaskAdvance, memory.WorkingPatch{})
		if err != nil {
			t.Fatalf("advance to %s: %v", want, err)
		}
		if cur.Stage != want {
			t.Fatalf("want %s got %s", want, cur.Stage)
		}
	}
}

func TestTaskMachineNoAdvanceWhilePaused(t *testing.T) {
	cur := memory.TaskState{Stage: memory.StageExecution, Paused: true, Step: "стены"}
	_, err := memory.ApplyTaskEvent(cur, memory.TaskAdvance, memory.WorkingPatch{})
	if err == nil {
		t.Fatal("advance on pause should fail")
	}
}

func TestInferTaskAdvancesWithCollectedData(t *testing.T) {
	idle := memory.WorkingState{Status: memory.WorkingIdle, Task: memory.TaskState{Stage: memory.StageIdle}}
	if p := memory.InferTaskPatch(idle, "привет"); p.Stage != "" || p.Event != "" {
		t.Fatalf("no goal → no stage: %+v", p)
	}

	plan := memory.WorkingState{
		Goal: "умный дом",
		Task: memory.TaskState{Stage: memory.StageIdle},
	}
	p := memory.InferTaskPatch(plan, "хочу умный дом")
	if p.Stage != memory.StagePlanning {
		t.Fatalf("goal → planning: %+v", p)
	}

	styleOnly := memory.WorkingState{
		Goal:        "умный дом",
		Constraints: []string{"коротко", "без воды"},
		Task:        memory.TaskState{Stage: memory.StagePlanning, Step: "собрать план", Expect: "согласовать план"},
	}
	p = memory.InferTaskPatch(styleOnly, "ну и ещё покороче")
	if p.Stage != "" {
		t.Fatalf("style constraints must not skip planning: %+v", p)
	}

	exec := memory.WorkingState{
		Goal:        "умный дом",
		Notes:       []string{"датчики", "бюджет"},
		Constraints: []string{"до 2 млн"},
		Task:        memory.TaskState{Stage: memory.StagePlanning, Step: "собрать план"},
	}
	p = memory.InferTaskPatch(exec, "приступай")
	if p.Stage != memory.StageExecution {
		t.Fatalf("accepted plan → execution: %+v", p)
	}

	val := exec
	val.Task.Stage = memory.StageExecution
	p = memory.InferTaskPatch(val, "проверь, всё ли сходится")
	if p.Stage != memory.StageValidation {
		t.Fatalf("check → validation: %+v", p)
	}

	done := val
	done.Task.Stage = memory.StageValidation
	p = memory.InferTaskPatch(done, "принимаю, можно закрывать")
	if p.Event != memory.TaskFinish && p.Stage != memory.StageDone {
		t.Fatalf("accept → done: %+v", p)
	}

	paused := exec
	paused.Task.Paused = true
	paused.Task.Stage = memory.StageExecution
	p = memory.InferTaskPatch(paused, "приступай")
	if p.Stage != "" {
		t.Fatalf("paused must not auto-advance: %+v", p)
	}
}

func TestInferTaskWalksOneStageAtATime(t *testing.T) {
	w := memory.WorkingState{
		Goal:      "кладовка",
		Notes:     []string{"учёт", "доступ"},
		Artifacts: map[string]string{"draft": "схема"},
		Task:      memory.TaskState{Stage: memory.StagePlanning, Step: "план"},
	}
	p := memory.InferTaskPatch(w, "проверь и закрывай")
	if p.Stage != memory.StageExecution {
		t.Fatalf("from planning jump at most one step, got %+v", p)
	}
}

func TestTaskFormatMentionsPauseAndDoneSoFar(t *testing.T) {
	block := memory.FormatTaskBlock(memory.TaskState{
		Stage:     memory.StageExecution,
		Step:      "собрать стены",
		Expect:    "подтвердить материал",
		Paused:    true,
		DoneSoFar: []string{"план: каркас"},
	})
	if !strings.Contains(block, "ПАУЗА") || !strings.Contains(block, "собрать стены") {
		t.Fatalf("pause block:\n%s", block)
	}
	if !strings.Contains(block, "не повторяй") || !strings.Contains(block, "план: каркас") {
		t.Fatalf("done_so_far missing:\n%s", block)
	}

	resume := memory.FormatTaskBlock(memory.TaskState{
		Stage:     memory.StageExecution,
		Step:      "собрать стены",
		LastEvent: memory.TaskResume,
	})
	if !strings.Contains(strings.ToLower(resume), "не начинай") {
		t.Fatalf("resume hint missing:\n%s", resume)
	}
}
