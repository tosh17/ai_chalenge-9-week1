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

// IllegalShift — попытка перейти в недопустимое состояние.
type IllegalShift struct {
	From    string `json:"from"`
	Want    string `json:"want"`
	Allowed string `json:"allowed"`
	Event   string `json:"event,omitempty"`
	Hit     string `json:"hit,omitempty"`
	Reason  string `json:"reason"`
}

// Transition — разрешённый переход жизненного цикла.
type Transition struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Event string `json:"event"`
	Title string `json:"title"`
}

// LifecycleTransitions — явная таблица переходов. Пауза этап не меняет.
func LifecycleTransitions() []Transition {
	return []Transition{
		{StageIdle, StagePlanning, TaskStart, "новая задача → планирование"},
		{StageDone, StagePlanning, TaskStart, "новая задача после закрытой → планирование"},
		{StagePlanning, StageExecution, TaskAdvance, "утверждённый план → выполнение"},
		{StageExecution, StageValidation, TaskAdvance, "есть результат → проверка"},
		{StageValidation, StageDone, TaskAdvance, "принято → готово"},
		{StageValidation, StageDone, TaskFinish, "принято → готово"},
		{StageValidation, StageExecution, TaskFail, "замечания → снова выполнение"},
		{StageExecution, StageExecution, TaskFail, "замечания → остаёмся в выполнении"},
	}
}

// AllowedStates — допустимые состояния задачи.
func AllowedStates() []string {
	return []string{StageIdle, StagePlanning, StageExecution, StageValidation, StageDone}
}

func allowedNextStages(from string, paused bool) []string {
	from = NormalizeStage(from)
	if paused {
		return []string{from}
	}
	var out []string
	seen := map[string]struct{}{}
	for _, tr := range LifecycleTransitions() {
		if tr.From != from {
			continue
		}
		if _, ok := seen[tr.To]; ok {
			continue
		}
		seen[tr.To] = struct{}{}
		out = append(out, tr.To)
	}
	return out
}

func allowedNextTitles(from string, paused bool) string {
	next := allowedNextStages(from, paused)
	if paused {
		return "снять паузу, остаться на «" + StageTitle(from) + "»"
	}
	if len(next) == 0 {
		if NormalizeStage(from) == StageDone {
			return "новая задача начнётся с планирования"
		}
		return "нет"
	}
	parts := make([]string, 0, len(next))
	for _, s := range next {
		parts = append(parts, "«"+StageTitle(s)+"»")
	}
	return strings.Join(parts, ", ")
}

func skipReason(from, to string) string {
	from, to = NormalizeStage(from), NormalizeStage(to)
	switch {
	case from == StagePlanning && to == StageExecution:
		return "нельзя делать реализацию до утверждённого плана"
	case from == StagePlanning && (to == StageValidation || to == StageDone):
		return "нельзя перепрыгнуть выполнение: сначала утвердите план"
	case from == StageExecution && to == StageDone:
		return "нельзя делать финал без проверки"
	case from == StageExecution && to == StagePlanning:
		return "нельзя вернуться к плану: этап уже выполнение"
	case from == StageIdle && to != StagePlanning:
		return "новую задачу можно начать только с планирования"
	case from == StageValidation && to == StagePlanning:
		return "нельзя перепрыгнуть назад к плану"
	default:
		return "из «" + StageTitle(from) + "» нельзя сразу в «" + StageTitle(to) + "»"
	}
}

// CanTransition проверяет, разрешён ли переход.
func CanTransition(from, to, event string, paused bool) error {
	from = NormalizeStage(from)
	to = NormalizeStage(to)
	event = strings.ToLower(strings.TrimSpace(event))
	if event == "" {
		event = TaskSet
	}
	if paused && event != TaskResume && event != TaskPause && event != TaskReset {
		if event == TaskAdvance || event == TaskFinish || event == TaskFail || event == TaskStart {
			return fmt.Errorf("сначала снимите паузу")
		}
		if to != StageIdle && to != from {
			return fmt.Errorf("на паузе нельзя менять этап (сейчас «%s»)", StageTitle(from))
		}
	}
	switch event {
	case TaskPause, TaskResume:
		if from == StageIdle {
			return fmt.Errorf("нет задачи, пауза невозможна")
		}
		if to != StageIdle && to != from {
			return fmt.Errorf("пауза не меняет этап")
		}
		return nil
	case TaskReset:
		return nil
	case TaskStart:
		if from == StageIdle || from == StageDone {
			if to == StageIdle || to == StagePlanning {
				return nil
			}
			return fmt.Errorf("%s", skipReason(from, to))
		}
		if to == from {
			return nil
		}
		return fmt.Errorf("%s", skipReason(from, to))
	case TaskAdvance:
		want := nextStage(from)
		if from == StageIdle {
			want = StagePlanning
		}
		if to == StageIdle || to == want {
			return nil
		}
		return fmt.Errorf("%s", skipReason(from, to))
	case TaskFail:
		if from != StageValidation && from != StageExecution {
			return fmt.Errorf("fail допустим на выполнении/проверке, сейчас «%s»", StageTitle(from))
		}
		if to != StageExecution && to != StageIdle {
			return fmt.Errorf("%s", skipReason(from, to))
		}
		return nil
	case TaskFinish:
		if from != StageValidation {
			return fmt.Errorf("%s", skipReason(from, StageDone))
		}
		return nil
	default:
		if to == StageIdle || to == from {
			return nil
		}
		if from == StageIdle && to == StagePlanning {
			return nil
		}
		if to == nextStage(from) {
			return nil
		}
		return fmt.Errorf("%s", skipReason(from, to))
	}
}

// GuardWorkingPatch убирает из патча запрещённый переход и возвращает отказ.
func GuardWorkingPatch(cur TaskState, p WorkingPatch) (WorkingPatch, *IllegalShift) {
	ev := strings.ToLower(strings.TrimSpace(p.Event))
	if p.Complete {
		ev = TaskFinish
		p.Event = TaskFinish
		p.Complete = false
	}
	if ev == "" {
		ev = TaskSet
	}
	to := NormalizeStage(p.Stage)
	if p.Stage == "" {
		switch ev {
		case TaskFinish:
			to = StageDone
		case TaskAdvance:
			to = nextStage(cur.Stage)
			if NormalizeStage(cur.Stage) == StageIdle {
				to = StagePlanning
			}
		case TaskFail:
			to = StageExecution
		case TaskStart:
			to = StagePlanning
		default:
			to = NormalizeStage(cur.Stage)
		}
	}
	if err := CanTransition(cur.Stage, to, ev, cur.Paused); err != nil {
		skip := &IllegalShift{
			From:    NormalizeStage(cur.Stage),
			Want:    to,
			Allowed: allowedNextTitles(cur.Stage, cur.Paused),
			Event:   ev,
			Reason:  err.Error(),
		}
		p.Stage = ""
		p.Complete = false
		switch ev {
		case TaskFinish, TaskAdvance, TaskFail, TaskStart:
			p.Event = ""
		}
		return p, skip
	}
	return p, nil
}

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
	origStage := t.Stage
	origPaused := t.Paused
	name := strings.ToLower(strings.TrimSpace(ev))
	if name == "" {
		name = TaskSet
	}

	switch name {
	case TaskReset:
		return TaskState{Stage: StageIdle}, nil

	case TaskStart:
		if err := CanTransition(origStage, StagePlanning, TaskStart, origPaused); err != nil {
			return t, err
		}
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
			return t, fmt.Errorf("задача уже на этапе готово")
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
			return t, fmt.Errorf("fail допустим на выполнении/проверке, сейчас «%s»", StageTitle(t.Stage))
		}
		t.Stage = StageExecution
		t.Paused = false
		if t.Expect == "" {
			t.Expect = "исправить замечания проверки"
		}

	case TaskFinish:
		if err := CanTransition(origStage, StageDone, TaskFinish, origPaused); err != nil {
			return t, err
		}
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
		if err := CanTransition(origStage, s, name, origPaused); err != nil {
			return cloneTask(cur), err
		}
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
	b.WriteString("=== СОСТОЯНИЕ ЗАДАЧИ (жизненный цикл) ===\n")
	b.WriteString("Допустимые состояния: нет задачи → планирование → выполнение → проверка → готово.\n")
	b.WriteString("Разрешённые переходы: только на соседний этап. Нельзя делать реализацию до утверждённого плана. Нельзя делать финал без проверки.\n")
	b.WriteString("Этап уже выставлен по полноте данных. Не обсуждай автомат и не проси переключить фазу.\n")
	stage := NormalizeStage(t.Stage)
	if stage == StageIdle {
		b.WriteString("Задачи нет. Если пользователь ставит цель — начни с планирования: уточни ТЗ, не делай работу.\n")
		return b.String()
	}
	b.WriteString("Сейчас фаза: ")
	b.WriteString(strings.ToUpper(StageTitle(stage)))
	b.WriteByte('\n')
	b.WriteString("отсюда можно: ")
	b.WriteString(allowedNextTitles(stage, t.Paused))
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
		b.WriteString("В этой фазе данных ещё не хватает. Спроси только тот факт, без которого нельзя начать. Если всё уже сказано — не переспрашивай.\n")
	case StageExecution:
		b.WriteString("В этой фазе: сразу делай задачу по уже собранным данным. Не собирай план заново и не спрашивай разрешение. Не закрывай задачу. Когда есть результат — предложи проверку.\n")
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

	if want == StageDone && cur != StageValidation {
		want = nextStage(cur)
	}
	if want == StageDone && cur == StageValidation {
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
	execCue := containsAny(low, "приступа", "делаем", "поехал", "поехали", "выполн",
		"берём в работу", "берем в работу", "начинай работ", "план ок", "план готов",
		"согласен с планом", "давай по плану", "к выполнению", "вся инфа", "все данные",
		"данных хватает", "можно делать")
	validCue := containsAny(low, "проверь", "проверк", "валид", "правильно ли",
		"посмотри результат", "готово к проверке", "все ли шаги", "всё ли сходится",
		"сверь", "посмотри что получилось", "что получилось", "это всё?", "это все?")
	doneCue := containsAny(low, "задача готова", "закрываем задачу", "можно закрывать",
		"всё сделано", "все сделано", "принимаю", "принято", "всё верно", "все верно",
		"закрывай", "на этом всё", "на этом все", "ок, готово", "можно закрыть")

	switch cur {
	case StagePlanning:
		if execCue || looksPlanApproved(low) || looksAccept(low) {
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

func looksPlanApproved(low string) bool {
	return containsAny(low, "приступа", "план ок", "план готов", "согласен с планом",
		"давай по плану", "к выполнению", "берём в работу", "берем в работу", "выполн")
}

func looksAccept(low string) bool {
	if low == "" {
		return false
	}
	phrases := []string{
		"ок", "окей", "хорошо", "да", "давай", "подходит", "норм", "согласен",
		"ладно", "пойдёт", "пойдет", "супер", "отлично", "верно", "ага",
		"да, план", "да план",
	}
	for _, p := range phrases {
		if low == p || low == p+"." || low == p+"!" ||
			strings.HasPrefix(low, p+" ") || strings.HasPrefix(low, p+",") ||
			strings.HasPrefix(low, p+".") || strings.HasPrefix(low, p+"!") ||
			strings.HasSuffix(low, " "+p) {
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

func parseRequestedStage(msg string) (stage, hit string) {
	trim := strings.TrimSpace(msg)
	low := strings.ToLower(trim)
	if strings.HasPrefix(low, "#этап") {
		body := strings.TrimSpace(trim[len("#этап"):])
		s := NormalizeStage(body)
		if s != StageIdle {
			return s, "#этап " + body
		}
	}
	switch {
	case containsAny(low, "сразу готов", "сразу в готов", "без провер", "без валид", "пропусти провер", "сразу закры", "закрывай задачу", "закрываем задачу"):
		return StageDone, "сразу готово / без проверки"
	case containsAny(low, "пиши код", "напиши код", "весь код", "пиши весь", "реализуй", "к реализации", "без плана", "пропусти план", "сразу делай", "сразу к работ", "сразу выполня"):
		return StageExecution, "реализация до плана"
	case containsAny(low, "сразу к провер", "пропусти выполн"):
		return StageValidation, "проверка до выполнения"
	}
	return "", ""
}

// FindIllegalShifts — запрос хочет недопустимый этап.
func FindIllegalShifts(t TaskState, userMsg string) []IllegalShift {
	cur := NormalizeStage(t.Stage)
	low := strings.ToLower(userMsg)
	var out []IllegalShift
	if t.Paused {
		if containsAny(low, "дальше", "#этап", "реализ", "закрывай", "в готов", "приступа") &&
			!containsAny(low, "продолж", "resume", "сними пауз") {
			out = append(out, IllegalShift{
				From:    cur,
				Want:    cur,
				Allowed: allowedNextTitles(cur, true),
				Hit:     "смена этапа на паузе",
				Reason:  "на паузе этап не меняется — сначала «продолжи», затем тот же шаг",
			})
		}
	}
	want, hit := parseRequestedStage(userMsg)
	if want == "" {
		return uniqueShifts(out)
	}
	if t.Paused {
		out = append(out, IllegalShift{
			From: cur, Want: want, Allowed: allowedNextTitles(cur, true), Hit: hit,
			Reason: "на паузе нельзя перейти в «" + StageTitle(want) + "»",
		})
		return uniqueShifts(out)
	}
	if err := CanTransition(cur, want, TaskSet, false); err != nil {
		out = append(out, IllegalShift{
			From:    cur,
			Want:    want,
			Allowed: allowedNextTitles(cur, false),
			Hit:     hit,
			Reason:  err.Error(),
		})
		return uniqueShifts(out)
	}
	// Ребро planning→execution существует, но реализация без утверждения плана — отказ.
	if cur == StagePlanning && want == StageExecution && !looksPlanApproved(low) {
		out = append(out, IllegalShift{
			From:    cur,
			Want:    want,
			Allowed: "утвердить план («приступай», «план ок»), затем выполнение",
			Hit:     hit,
			Reason:  skipReason(cur, want),
		})
	}
	return uniqueShifts(out)
}

func uniqueShifts(in []IllegalShift) []IllegalShift {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]IllegalShift, 0, len(in))
	for _, s := range in {
		key := s.From + "|" + s.Want + "|" + s.Reason
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}

// FormatSkipBlock — отказ, если запрос перепрыгивает этап.
func FormatSkipBlock(skips []IllegalShift) string {
	if len(skips) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("=== НЕДОПУСТИМЫЙ ПЕРЕХОД ===\n")
	b.WriteString("Запрос хочет перепрыгнуть этап. Ты ОБЯЗАН отказать в этой работе.\n")
	b.WriteString("Не пиши реализацию, если план не утверждён. Не закрывай задачу без проверки.\n")
	b.WriteString("Оставайся в текущей фазе. Объясни, какой переход запрещён и что нужно сделать сейчас.\n")
	for _, s := range skips {
		b.WriteString("- сейчас «")
		b.WriteString(StageTitle(s.From))
		b.WriteString("», хотели «")
		b.WriteString(StageTitle(s.Want))
		b.WriteString("»: ")
		b.WriteString(s.Reason)
		b.WriteByte('\n')
		if s.Allowed != "" {
			b.WriteString("  можно: ")
			b.WriteString(s.Allowed)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
