package s1

import (
	"strings"
	"testing"
)

func TestLogsRejectsInjection(t *testing.T) {
	if _, err := Logs("gitlab;rm", 10); err == nil {
		t.Fatal("semicolon in name must fail")
	}
	argv, err := Logs("gitlab", 80)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkArgv(argv); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joinQuoted(argv), ";") {
		t.Fatal(joinQuoted(argv))
	}
}

func TestClosedSubcommand(t *testing.T) {
	err := checkArgv([]string{"sudo", "-n", "docker", "rm", "-f", "gitlab"})
	if err == nil {
		t.Fatal("rm must be refused")
	}
	if _, err := DemoRun("gitlab", "nginx:alpine", 18080); err == nil {
		t.Fatal("gitlab name")
	}
	if _, err := DemoRun("ops-nginx", "nginx:latest", 18080); err == nil {
		t.Fatal("image")
	}
	if _, err := DemoRun("ops-nginx", "nginx:alpine", 80); err == nil {
		t.Fatal("port")
	}
	argv, err := DemoRun("ops-nginx", "nginx:alpine", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkArgv(append(argv, "--privileged")); err == nil {
		t.Fatal("privileged")
	}
	rm, err := DemoRemove("ops-nginx")
	if err != nil || checkArgv(rm) != nil {
		t.Fatal(err)
	}
}

func TestRedact(t *testing.T) {
	got := Redact("token = glpat-secretvalue\nok")
	if strings.Contains(got, "glpat-secretvalue") || !strings.Contains(got, "<redacted>") {
		t.Fatal(got)
	}
}
