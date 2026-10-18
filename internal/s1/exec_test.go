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
}

func TestRedact(t *testing.T) {
	got := Redact("token = glpat-secretvalue\nok")
	if strings.Contains(got, "glpat-secretvalue") || !strings.Contains(got, "<redacted>") {
		t.Fatal(got)
	}
}
