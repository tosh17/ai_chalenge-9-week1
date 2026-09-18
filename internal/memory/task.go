package memory

import (
	"fmt"
	"strings"
)

// Этапы задачи — конечный автомат рабочей памяти.
const (
	StageIdle       = "idle"
	StagePlanning   = "planning"
	StageExecution  = "execution"
	StageValidation = "validation"
	StageDone       = "done"
)

// События автомата.
const (
	TaskStart   = "start"
	TaskPause   = "pause"
	TaskResume  = "resume"
	TaskAdvance = "advance"
	TaskFail    = "fail"
	TaskFinish  = "finish"
	TaskSet     = "set"
	TaskReset   = "reset"
)

var stageOrder = []string{StagePlanning, StageExecution, StageValidation, StageDone}

// TaskState — формализованное состояние текущей задачи.
type TaskState struct {
	Stage     string   `json:"stage"`
	Step      string   `json:"step,omitempty"`
	StepIndex int      `json:"step_index,omitempty"`
	Expect    string   `json:"expect,omitempty"`
	Paused    bool     `json:"paused"`
	DoneSoFar []string `json:"done_so_far,omitempty"`
	LastEvent string   `json:"last_event,omitempty"`
}

// Empty — нет живой задачи.
func (t TaskState) Empty() bool {
	return NormalizeStage(t.Stage) == StageIdle && t.Step == "" && t.Expect == "" && !t.Paused
}

// NormalizeStage приводит строку к известному этапу.
func NormalizeStage(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case StagePlanning, "план", "планирование":
		return StagePlanning
	case StageExecution, "exec", "работа", "выполнение":
		return StageExecution
	case StageValidation, "check", "проверка", "валидация":
		return StageValidation
	case StageDone, "готово", "закрыта", "complete":
		return StageDone
	case StageIdle, "", "нет":
		return StageIdle
	default:
		return StageIdle
	}
}

func defaultExpect(stage string) string {
	switch NormalizeStage(stage) {
	case StagePlanning:
		return "согласовать план"
	case StageExecution:
		return "сделать текущий шаг плана"
	case StageValidation:
		return "подтвердить, что результат сходится с целью"
	case StageDone:
		return "задача закрыта"
	default:
		return ""
	}
}

func nextStage(stage string) string {
	cur := NormalizeStage(stage)
	for i, s := range stageOrder {
		if s == cur && i+1 < len(stageOrder) {
			return stageOrder[i+1]
		}
	}
	if cur == StageIdle {
		return StagePlanning
	}
	return StageDone
}

func cloneTask(in TaskState) TaskState {
	out := in
	if in.DoneSoFar != nil {
		out.DoneSoFar = append([]string{}, in.DoneSoFar...)
	}
	out.Stage = NormalizeStage(in.Stage)
	return out
}

// ApplyTaskEvent применяет событие к копии состояния.
func ApplyTaskEvent(cur TaskState, ev string, patch WorkingPatch) (TaskState, error) {
	t := cloneTask(cur)
	name := strings.ToLower(strings.TrimSpace(ev))
	if name == "" {
		name = TaskSet
	}

	switch name {
	case TaskReset:
		return TaskState{Stage: StageIdle}, nil

	case TaskStart:
		if t.Stage == StageIdle || t.Stage == StageDone {
			t.Stage = StagePlanning
			t.Paused = false
			t.StepIndex = 1
			if t.Step == "" {
				t.Step = "собрать план"
			}
			if t.Expect == "" {
				t.Expect = defaultExpect(t.Stage)
			}
		}
		t.Paused = false

	case TaskPause:
		if t.Stage != StageIdle {
			t.Paused = true
		}

	case TaskResume:
		t.Paused = false

	case TaskAdvance:
		if t.Paused {
			return t, fmt.Errorf("сначала снимите паузу")
		}
		if t.Stage == StageIdle {
			t.Stage = StagePlanning
		} else if t.Stage == StageDone {
			return t, fmt.Errorf("задача уже на этапе done")
		} else {
			if t.Step != "" {
				t.DoneSoFar = appendUnique(t.DoneSoFar, t.Stage+": "+t.Step)
			}
			t.Stage = nextStage(t.Stage)
			t.StepIndex++
		}
		t.Expect = defaultExpect(t.Stage)
		if t.Stage == StageDone {
			t.Paused = false
			t.Expect = defaultExpect(StageDone)
		}

	case TaskFail:
		if t.Stage != StageValidation && t.Stage != StageExecution {
			return t, fmt.Errorf("fail допустим на execution/validation, сейчас %s", t.Stage)
		}
		t.Stage = StageExecution
		t.Paused = false
		if t.Expect == "" {
			t.Expect = "исправить замечания проверки"
		}

	case TaskFinish:
		if t.Step != "" {
			t.DoneSoFar = appendUnique(t.DoneSoFar, t.Stage+": "+t.Step)
		}
		t.Stage = StageDone
		t.Paused = false
		t.Expect = defaultExpect(StageDone)

	case TaskSet:
		// поля из патча ниже
	default:
		return t, fmt.Errorf("неизвестное событие задачи %q", ev)
	}

	if s := NormalizeStage(patch.Stage); patch.Stage != "" && s != StageIdle {
		t.Stage = s
	}
	if patch.Step != "" {
		t.Step = strings.TrimSpace(patch.Step)
	}
	if patch.StepIndex > 0 {
		t.StepIndex = patch.StepIndex
	}
	if patch.Expect != "" {
		t.Expect = strings.TrimSpace(patch.Expect)
	}
	if len(patch.AddDone) > 0 {
		t.DoneSoFar = appendUnique(t.DoneSoFar, patch.AddDone...)
	}
	if t.Stage != StageIdle && t.Expect == "" {
		t.Expect = defaultExpect(t.Stage)
	}
	t.LastEvent = name
	return t, nil
}

// StageTitle — человеческое имя этапа.
func StageTitle(s string) string {
	switch NormalizeStage(s) {
	case StagePlanning:
		return "планирование"
	case StageExecution:
		return "выполнение"
	case StageValidation:
		return "проверка"
	case StageDone:
		return "готово"
	default:
		return "нет задачи"
	}
}

// FormatTaskBlock — отдельный system-блок автомата для каждого запроса.
func FormatTaskBlock(t TaskState) string {
	var b strings.Builder
	b.WriteString("=== СОСТОЯНИЕ ЗАДАЧИ (конечный автомат) ===\n")
	b.WriteString("Этапы строго по порядку: планирование → выполнение → проверка → готово.\n")
	b.WriteString("Не перескакивай и не ходи назад. Этап ставит система, не пользователь.\n")
	stage := NormalizeStage(t.Stage)
	if stage == StageIdle {
		b.WriteString("Задачи нет. Если пользователь ставит цель — начни с планирования: уточни ТЗ, не делай работу.\n")
		return b.String()
	}
	b.WriteString("Сейчас фаза: ")
	b.WriteString(strings.ToUpper(StageTitle(stage)))
	b.WriteByte('\n')
	if t.Step != "" {
		b.WriteString("текущий шаг")
		if t.StepIndex > 0 {
			fmt.Fprintf(&b, " (%d)", t.StepIndex)
		}
		b.WriteString(": ")
		b.WriteString(t.Step)
		b.WriteByte('\n')
	}
	if t.Expect != "" {
		b.WriteString("ожидаемое действие: ")
		b.WriteString(t.Expect)
		b.WriteByte('\n')
	}
	if t.Paused {
		b.WriteString("ПАУЗА на этом этапе. Коротко подтверди паузу и жди «продолжи». Не продолжай работу, не пересказывай план.\n")
	} else if t.LastEvent == TaskResume {
		b.WriteString("ТОЛЬКО ЧТО СНЯТА ПАУЗА. Продолжай с текущего шага и ожидаемого действия. Не начинай «итак, напомню план».\n")
	}
	switch stage {
	case StagePlanning:
		b.WriteString("В этой фазе: только уточняй цель и собирай план. Не пиши готовое решение. В конце спроси, можно ли переходить к выполнению.\n")
	case StageExecution:
		b.WriteString("В этой фазе: делай по согласованному плану. Не начинай планирование заново. Не закрывай задачу. Когда есть результат — предложи проверку.\n")
	case StageValidation:
		b.WriteString("В этой фазе: сверь результат с целью и ограничениями, перечисли пробелы. Не планируй и не делай новую работу. Закрывать можно только после подтверждения пользователя.\n")
	case StageDone:
		b.WriteString("Задача закрыта. Коротко подтверди. Новый запрос без новой цели не перезапускает работу.\n")
	}
	if len(t.DoneSoFar) > 0 {
		b.WriteString("уже сделано (не повторяй, не объясняй заново):\n")
		for _, s := range t.DoneSoFar {
			b.WriteString("- ")
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func stageRank(s string) int {
	switch NormalizeStage(s) {
	case StagePlanning:
		return 1
	case StageExecution:
		return 2
	case StageValidation:
		return 3
	case StageDone:
		return 4
	default:
		return 0
	}
}

func containsAny(low string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func defaultStep(stage string, w WorkingState) string {
	switch NormalizeStage(stage) {
	case StagePlanning:
		if w.Goal != "" {
			return "уточнить план: " + truncateRunes(w.Goal, 48)
		}
		return "собрать план"
	case StageExecution:
		return "выполнить согласованный план"
	case StageValidation:
		return "проверить результат по цели и ограничениям"
	case StageDone:
		return "задача закрыта"
	default:
		return ""
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// InferTaskPatch сам ставит этап по собранным данным и реплике. Пользователь этап не выбирает.
// Двигается только вперёд и не больше чем на один шаг за реплику:
// планирование → выполнение → проверка → готово.
func InferTaskPatch(w WorkingState, userMsg string) WorkingPatch {
	if w.Task.Paused {
		return WorkingPatch{}
	}
	cur := NormalizeStage(w.Task.Stage)
	low := strings.ToLower(userMsg)
	hasGoal := strings.TrimSpace(w.Goal) != ""
	if !hasGoal && cur == StageIdle {
		return WorkingPatch{}
	}

	if looksFail(low) && (cur == StageValidation || cur == StageDone) {
		return WorkingPatch{Event: TaskFail, Step: "исправить замечания", Expect: "внести правки"}
	}

	want := desiredStage(w, userMsg, cur)
	if stageRank(want) < stageRank(cur) {
		want = cur
	}
	if stageRank(want) > stageRank(cur)+1 {
		want = nextStage(cur)
	}

	if want == StageDone && cur != StageDone {
		p := WorkingPatch{Event: TaskFinish, Step: defaultStep(StageDone, w), Expect: defaultExpect(StageDone)}
		if w.Task.Step != "" {
			p.AddDone = []string{cur + ": " + w.Task.Step}
		}
		return p
	}

	if want == cur {
		if w.Task.Step != "" && w.Task.Expect != "" {
			return WorkingPatch{}
		}
		p := WorkingPatch{Event: TaskSet}
		if w.Task.Step == "" {
			p.Step = defaultStep(cur, w)
		}
		if w.Task.Expect == "" {
			p.Expect = defaultExpect(cur)
		}
		if p.Step == "" && p.Expect == "" {
			return WorkingPatch{}
		}
		return p
	}

	p := WorkingPatch{
		Event:  TaskSet,
		Stage:  want,
		Step:   defaultStep(want, w),
		Expect: defaultExpect(want),
	}
	if w.Task.Step != "" && want != cur {
		p.AddDone = []string{StageTitle(cur) + ": " + w.Task.Step}
	}
	return p
}

func desiredStage(w WorkingState, userMsg, cur string) string {
	low := strings.ToLower(strings.TrimSpace(userMsg))
	hasGoal := strings.TrimSpace(w.Goal) != ""
	if !hasGoal && cur == StageIdle {
		return StageIdle
	}
	if hasGoal && (cur == StageIdle || cur == "") {
		return StagePlanning
	}

	signals := taskSignalCount(w)
	hasArtifact := len(w.Artifacts) > 0
	execCue := containsAny(low, "приступа", "делаем", "поехал", "поехали", "выполня",
		"берём в работу", "берем в работу", "начинай работ", "план ок", "план готов",
		"согласен с планом", "давай по плану", "к выполнению")
	validCue := containsAny(low, "проверь", "проверк", "валид", "правильно ли",
		"посмотри результат", "готово к проверке", "все ли шаги", "всё ли сходится",
		"сверь", "посмотри что получилось", "что получилось", "это всё?", "это все?")
	doneCue := containsAny(low, "задача готова", "закрываем задачу", "можно закрывать",
		"всё сделано", "все сделано", "принимаю", "принято", "всё верно", "все верно",
		"закрывай", "на этом всё", "на этом все", "ок, готово", "можно закрыть")

	switch cur {
	case StagePlanning:
		if execCue || looksAccept(low) && signals >= 1 || signals >= 3 {
			return StageExecution
		}
		return StagePlanning
	case StageExecution:
		if validCue || hasArtifact || doneCue || looksAccept(low) && signals >= 2 {
			return StageValidation
		}
		return StageExecution
	case StageValidation:
		if doneCue || looksAccept(low) {
			return StageDone
		}
		return StageValidation
	case StageDone:
		return StageDone
	default:
		if hasGoal {
			return StagePlanning
		}
		return StageIdle
	}
}

func looksFail(low string) bool {
	return containsAny(low, "переделай", "исправь", "не то,", "неверно", "не правильно", "это не то")
}

func looksAccept(low string) bool {
	if low == "" {
		return false
	}
	phrases := []string{
		"ок", "окей", "хорошо", "давай", "подходит", "норм", "согласен",
		"ладно", "пойдёт", "пойдет", "супер", "отлично", "да, план", "да план",
	}
	for _, p := range phrases {
		if low == p || strings.HasPrefix(low, p+" ") || strings.HasPrefix(low, p+",") ||
			strings.HasPrefix(low, p+"!") || strings.HasSuffix(low, " "+p) {
			return true
		}
	}
	return false
}

func taskSignalCount(w WorkingState) int {
	n := 0
	for _, note := range w.Notes {
		low := strings.ToLower(strings.TrimSpace(note))
		if low == "" || strings.HasPrefix(low, "пауза:") || strings.HasPrefix(low, "resume:") {
			continue
		}
		n++
	}
	for _, c := range w.Constraints {
		if isStyleish(c) {
			continue
		}
		n++
	}
	n += len(w.Artifacts)
	return n
}

func isStyleish(s string) bool {
	return containsAny(strings.ToLower(s), "коротко", "кратко", "без воды", "без жаргона",
		"без эмодзи", "списк", "предложени", "формат", "стиль")
}
