package dock

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/s1"
)

func TestFallbackStartsNginx(t *testing.T) {
	root := t.TempDir()
	buf := &s1.BufferRunner{Out: map[string]string{
		"ps":     "gitlab|running|8929",
		"images": "gitlab/gitlab-ce:19.4.1-ce.0|abc|2GB",
		"run":    "container-id",
	}}
	res, err := (&Assistant{Tools: Local{Tools: Handlers(buf, root)}}).Run(context.Background(), "Покажи контейнеры и подними простую страницу на 18080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Reply, "## Рекомендация") || !strings.Contains(res.Reply, "nginx:alpine") {
		t.Fatal(res.Reply)
	}
	body, err := os.ReadFile(filepath.Join(root, "ops-nginx.yml"))
	if err != nil || !strings.Contains(string(body), "nginx:alpine") {
		t.Fatalf("%s %v", body, err)
	}
	if len(buf.Calls) != 3 || buf.Calls[2][3] != "run" || buf.Calls[2][6] != "ops-nginx" {
		t.Fatalf("%#v", buf.Calls)
	}
}

func TestDoesNotStartWhenAskedNotTo(t *testing.T) {
	buf := &s1.BufferRunner{Out: map[string]string{"ps": "ollama|running|11434", "images": "nginx:alpine|def|40MB"}}
	res, err := (&Assistant{Tools: Local{Tools: Handlers(buf, t.TempDir())}}).Run(context.Background(), "Порекомендуй образ для статики, не запускай")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Reply, "18080") && strings.Contains(res.Reply, "поднят") {
		t.Fatal(res.Reply)
	}
	if !strings.Contains(res.Reply, "уже лежащий nginx:alpine") {
		t.Fatal(res.Reply)
	}
	for _, call := range buf.Calls {
		if call[3] == "run" || call[3] == "rm" {
			t.Fatal(call)
		}
	}
}

func TestRemoveOnlyDemo(t *testing.T) {
	buf := &s1.BufferRunner{Out: map[string]string{"ps": "ops-nginx|running|18080", "images": "nginx:alpine|def|40MB", "rm": "ops-nginx"}}
	_, err := (&Assistant{Tools: Local{Tools: Handlers(buf, t.TempDir())}}).Run(context.Background(), "Убери контейнер ops-nginx")
	if err != nil {
		t.Fatal(err)
	}
	var removed bool
	for _, call := range buf.Calls {
		if call[3] == "rm" && call[len(call)-1] == "ops-nginx" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("%#v", buf.Calls)
	}
}
