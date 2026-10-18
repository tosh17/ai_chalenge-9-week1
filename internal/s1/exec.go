package s1

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Runner выполняет уже проверенную команду: локально или по SSH на s1.
type Runner interface {
	Run(ctx context.Context, argv []string) (string, error)
}

// Exec — docker через sudo -n. Host пустой значит эта же машина, иначе ssh user@host.
type Exec struct {
	Host    string
	Timeout time.Duration
}

func (e Exec) Run(ctx context.Context, argv []string) (string, error) {
	if err := checkArgv(argv); err != nil {
		return "", err
	}
	limit := e.Timeout
	if limit <= 0 {
		limit = 25 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var cmd *exec.Cmd
	if e.Host == "" {
		cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", e.Host, joinQuoted(argv))
	}
	out, err := cmd.CombinedOutput()
	text := Redact(strings.TrimSpace(string(out)))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("%s", text)
	}
	return text, nil
}

var (
	nameRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	demoRE    = regexp.MustCompile(`^ops-[a-z0-9-]{1,24}$`)
	argRE     = regexp.MustCompile(`^[A-Za-z0-9_.:/=@{}*,+| -]+$`)
	publishRE = regexp.MustCompile(`^10\.10\.1\.101:180(8[0-9]|90):(80|8080)$`)
	demoImage = map[string]bool{"nginx:alpine": true, "alpine:3.20": true}
)

func checkArgv(argv []string) error {
	if len(argv) < 4 || argv[0] != "sudo" || argv[1] != "-n" || argv[2] != "docker" {
		return fmt.Errorf("разрешён только sudo -n docker")
	}
	switch argv[3] {
	case "ps", "logs", "inspect", "images":
		for _, arg := range argv[4:] {
			if !argRE.MatchString(arg) {
				return fmt.Errorf("небезопасный аргумент")
			}
		}
		return nil
	case "run":
		return checkRun(argv[4:])
	case "rm", "stop":
		return checkDemo(argv[3], argv[4:])
	default:
		return fmt.Errorf("команда docker %s закрыта", argv[3])
	}
}

func checkRun(args []string) error {
	if len(args) < 6 || args[0] != "-d" || args[1] != "--name" || args[3] != "--restart" || args[4] != "no" {
		return fmt.Errorf("форма docker run закрыта")
	}
	if !demoRE.MatchString(args[2]) {
		return fmt.Errorf("имя должно начинаться с ops-")
	}
	rest := args[5:]
	if len(rest) >= 2 && rest[0] == "-p" {
		if !publishRE.MatchString(rest[1]) {
			return fmt.Errorf("порт только 18080–18090 на 10.10.1.101")
		}
		rest = rest[2:]
	}
	if len(rest) == 0 || !demoImage[rest[0]] {
		return fmt.Errorf("образ не из списка")
	}
	if rest[0] == "alpine:3.20" {
		if len(rest) != 3 || rest[1] != "sleep" || rest[2] != "3600" {
			return fmt.Errorf("для alpine разрешён только sleep 3600")
		}
		return nil
	}
	if len(rest) != 1 {
		return fmt.Errorf("лишние аргументы")
	}
	return nil
}

func checkDemo(sub string, args []string) error {
	name := ""
	switch sub {
	case "rm":
		if len(args) != 2 || args[0] != "-f" {
			return fmt.Errorf("форма docker rm закрыта")
		}
		name = args[1]
	case "stop":
		if len(args) != 1 {
			return fmt.Errorf("форма docker stop закрыта")
		}
		name = args[0]
	}
	if !demoRE.MatchString(name) {
		return fmt.Errorf("останавливать можно только контейнер ops-")
	}
	return nil
}

// DemoRun собирает docker run только для учебного контейнера ops-*.
func DemoRun(name, image string, port int) ([]string, error) {
	if !demoImage[image] {
		return nil, fmt.Errorf("образ не из списка")
	}
	inside := 80
	if image == "alpine:3.20" {
		inside = 8080
	}
	publish := fmt.Sprintf("10.10.1.101:%d:%d", port, inside)
	argv := []string{"sudo", "-n", "docker", "run", "-d", "--name", name, "--restart", "no", "-p", publish, image}
	if image == "alpine:3.20" {
		argv = append(argv, "sleep", "3600")
	}
	if err := checkArgv(argv); err != nil {
		return nil, err
	}
	return argv, nil
}

// DemoRemove удаляет только контейнер с префиксом ops-.
func DemoRemove(name string) ([]string, error) {
	argv := []string{"sudo", "-n", "docker", "rm", "-f", name}
	if err := checkArgv(argv); err != nil {
		return nil, err
	}
	return argv, nil
}

func joinQuoted(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(parts, " ")
}

// ListServices — docker ps по всем контейнерам.
func ListServices() []string {
	return []string{"sudo", "-n", "docker", "ps", "-a", "--format", "{{.Names}}|{{.Status}}|{{.Ports}}"}
}

// ListImages — локальные образы.
func ListImages() []string {
	return []string{"sudo", "-n", "docker", "images", "--format", "{{.Repository}}:{{.Tag}}|{{.ID}}|{{.Size}}"}
}

// Logs — хвост логов одного контейнера.
func Logs(name string, tail int) ([]string, error) {
	if !nameRE.MatchString(name) {
		return nil, fmt.Errorf("имя контейнера")
	}
	if tail < 1 || tail > 200 {
		tail = 80
	}
	return []string{"sudo", "-n", "docker", "logs", "--tail", fmt.Sprintf("%d", tail), name}, nil
}

// Status — состояние контейнера.
func Status(name string) ([]string, error) {
	if !nameRE.MatchString(name) {
		return nil, fmt.Errorf("имя контейнера")
	}
	return []string{"sudo", "-n", "docker", "inspect", "--format", "{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", name}, nil
}

var secretRE = regexp.MustCompile(`(?i)(token|password|secret|api_key)(\s*[:=]\s*)\S+`)

// Redact убирает значения, похожие на секреты, из вывода команд.
func Redact(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return secretRE.ReplaceAllString(s, "$1$2<redacted>")
}

// Clip обрезает длинный вывод.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Useful so tests can compare command lines without spaces tricks.
func Format(argv []string) string {
	return strings.Join(argv, " ")
}

// BufferRunner запоминает команды и отвечает заготовками. Для тестов.
type BufferRunner struct {
	Calls [][]string
	Out   map[string]string
	Err   map[string]string
}

func (b *BufferRunner) Run(_ context.Context, argv []string) (string, error) {
	if err := checkArgv(argv); err != nil {
		return "", err
	}
	cp := append([]string(nil), argv...)
	b.Calls = append(b.Calls, cp)
	key := argv[3]
	if argv[3] == "logs" || argv[3] == "inspect" {
		key = argv[3] + ":" + argv[len(argv)-1]
	}
	if msg, ok := b.Err[key]; ok {
		return msg, fmt.Errorf("%s", msg)
	}
	if text, ok := b.Out[key]; ok {
		return text, nil
	}
	return "", nil
}

// HasDocker сообщает, похож ли вывод на отказ sudo.
func HasDocker(text string) bool {
	return bytes.Contains([]byte(text), []byte("docker"))
}
