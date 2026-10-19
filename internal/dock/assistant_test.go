package dock

import (
	"context"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/s1"
)

func TestModelAsksStatsForResources(t *testing.T) {
	buf := &s1.BufferRunner{Out: map[string]string{"stats": "gitlab|4.2%|1.8GiB / 14.99GiB"}}
	chat := &scripted{steps: []deepseek.ChatResult{
		{ToolCalls: []deepseek.ToolCall{{ID: "1", Name: "service_stats", Arguments: `{"name":"gitlab"}`}}},
		{Reply: "GitLab сейчас берёт 4.2% CPU и 1.8 ГиБ памяти."},
	}}
	history := []Turn{
		{Role: "user", Content: "какие контейнеры запущены?"},
		{Role: "assistant", Content: "gitlab Up, board Up"},
		{Role: "user", Content: "board это что"},
		{Role: "assistant", Content: "board работает на 8090"},
		{Role: "user", Content: "понял"},
	}
	res, err := (&Assistant{Chat: chat, Tools: Local{Tools: Handlers(buf, t.TempDir())}}).Reply(context.Background(), "gitlab какие ресурсы потребляет?", history)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "GitLab сейчас берёт 4.2% CPU и 1.8 ГиБ памяти." {
		t.Fatal(res.Reply)
	}
	if len(chat.seen) == 0 || !messagesHold(chat.seen[0], history) {
		t.Fatalf("%#v", chat.seen)
	}
	var stats bool
	for _, call := range buf.Calls {
		if len(call) > 3 && call[3] == "stats" && call[len(call)-1] == "gitlab" {
			stats = true
		}
		if len(call) > 3 && call[3] == "inspect" {
			t.Fatalf("info instead of stats: %#v", call)
		}
	}
	if !stats {
		t.Fatalf("%#v", buf.Calls)
	}
}

func TestHistoryKeepsAtLeastFive(t *testing.T) {
	chat := &scripted{steps: []deepseek.ChatResult{{Reply: "уточни контейнер"}}}
	var history []Turn
	for i := 0; i < 10; i++ {
		history = append(history, Turn{Role: "user", Content: "реплика-" + string(rune('a'+i))})
	}
	_, err := (&Assistant{Chat: chat, Tools: Local{Tools: Handlers(&s1.BufferRunner{}, t.TempDir())}}).Reply(context.Background(), "и что дальше", history)
	if err != nil {
		t.Fatal(err)
	}
	blob := messageBlob(chat.seen[0])
	if strings.Contains(blob, "реплика-a") || strings.Contains(blob, "реплика-b") {
		t.Fatal(blob)
	}
	kept := 0
	for _, turn := range history[len(history)-historyKeep:] {
		if strings.Contains(blob, turn.Content) {
			kept++
		}
	}
	if kept < historyMin {
		t.Fatalf("kept %d\n%s", kept, blob)
	}
}

func TestModelCanAsk(t *testing.T) {
	buf := &s1.BufferRunner{}
	chat := &scripted{steps: []deepseek.ChatResult{{Reply: "Про какой контейнер посмотреть ресурсы?"}}}
	res, err := (&Assistant{Chat: chat, Tools: Local{Tools: Handlers(buf, t.TempDir())}}).Run(context.Background(), "какие ресурсы")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "Про какой контейнер посмотреть ресурсы?" || len(buf.Calls) != 0 {
		t.Fatalf("%s %#v", res.Reply, buf.Calls)
	}
}

func TestModelStartsDemo(t *testing.T) {
	buf := &s1.BufferRunner{Out: map[string]string{"run": "id"}}
	chat := &scripted{steps: []deepseek.ChatResult{
		{ToolCalls: []deepseek.ToolCall{{ID: "1", Name: "run_demo", Arguments: `{"name":"ops-page","image":"nginx:alpine","port":18081}`}}},
		{Reply: "Поднял пустую страницу на порту 18081."},
	}}
	res, err := (&Assistant{Chat: chat, Tools: Local{Tools: Handlers(buf, t.TempDir())}}).Run(context.Background(), "можем поднять новый контейнер с пустой страничкой")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "Поднял пустую страницу на порту 18081." {
		t.Fatal(res.Reply)
	}
	var ran bool
	for _, call := range buf.Calls {
		if len(call) > 6 && call[3] == "run" && call[6] == "ops-page" {
			ran = true
		}
	}
	if !ran {
		t.Fatalf("%#v", buf.Calls)
	}
}

func TestModelCannotRemoveGitlab(t *testing.T) {
	buf := &s1.BufferRunner{Out: map[string]string{"rm": "gone"}}
	chat := &scripted{steps: []deepseek.ChatResult{
		{ToolCalls: []deepseek.ToolCall{{ID: "1", Name: "remove_demo", Arguments: `{"name":"gitlab"}`}}},
		{Reply: "GitLab не удаляю."},
	}}
	res, err := (&Assistant{Chat: chat, Tools: Local{Tools: Handlers(buf, t.TempDir())}}).Run(context.Background(), "останови gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "GitLab не удаляю." {
		t.Fatal(res.Reply)
	}
	for _, call := range buf.Calls {
		if len(call) > 3 && call[3] == "rm" {
			t.Fatalf("%#v", call)
		}
	}
	if len(res.Calls) != 1 || res.Calls[0].Err == "" {
		t.Fatalf("%#v", res.Calls)
	}
}

func TestNoModel(t *testing.T) {
	_, err := (&Assistant{Tools: Local{Tools: Handlers(&s1.BufferRunner{}, t.TempDir())}}).Run(context.Background(), "что запущено")
	if err == nil || !strings.Contains(err.Error(), "модель") {
		t.Fatal(err)
	}
}

type scripted struct {
	steps []deepseek.ChatResult
	seen  [][]deepseek.Message
}

func (s *scripted) ChatTools(_ context.Context, messages []deepseek.Message, _ []deepseek.Tool) (deepseek.ChatResult, error) {
	cp := append([]deepseek.Message(nil), messages...)
	s.seen = append(s.seen, cp)
	if len(s.seen) > len(s.steps) {
		return deepseek.ChatResult{Reply: "готово"}, nil
	}
	return s.steps[len(s.seen)-1], nil
}

func messagesHold(messages []deepseek.Message, history []Turn) bool {
	blob := messageBlob(messages)
	for _, turn := range history {
		if !strings.Contains(blob, turn.Content) {
			return false
		}
	}
	return strings.Contains(blob, "gitlab какие ресурсы потребляет?")
}

func messageBlob(messages []deepseek.Message) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(msg.Role)
		b.WriteString(":")
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	return b.String()
}
