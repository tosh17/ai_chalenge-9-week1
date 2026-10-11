package agent

import (
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

func TestScoreTune(t *testing.T) {
	facts := [][]string{{"молок"}, {"какао"}, {"1½", "1,5", "полтор"}}
	quotes := "1 1/2 tablespoons cocoa, 2 cups milk"
	quality, hit, grounded := scoreTune("В какао 2 чашки молока и полторы ложки какао. Источник: [1]", quotes, facts)
	if hit != 3 || !grounded || quality != 100 {
		t.Fatalf("quality=%d hit=%d grounded=%v", quality, hit, grounded)
	}
	empty, _, ok := scoreTune("   ", quotes, facts)
	if empty != 0 || ok {
		t.Fatalf("пустой ответ засчитан: %d %v", empty, ok)
	}
	invented, inventedHit, inventedOK := scoreTune("Нужно 9 чашек молока и какао.", quotes, facts)
	if inventedOK || inventedHit != 2 || invented >= 100 {
		t.Fatalf("выдуманное число не наказано: q=%d hit=%d grounded=%v", invented, inventedHit, inventedOK)
	}
}

func TestQuantNoteKeepsSingleQuant(t *testing.T) {
	note := quantNote([]deepseek.ModelFile{{
		Name:   "qwen-local:latest",
		Quant:  "Q4_K_M",
		Params: "9.7B",
		Bytes:  6603314913,
	}}, "qwen-local", nil)
	for _, part := range []string{"Q4_K_M", "эта модель в чате", "Q8 и FP16", "6.6 ГБ"} {
		if !strings.Contains(note, part) {
			t.Fatalf("нет %q в %s", part, note)
		}
	}
}

func TestValidateLocalSettings(t *testing.T) {
	ok := LocalSettings{Temperature: 0.15, MaxTokens: 320, ContextTokens: 8192, Quant: "Q4_K_M", Prompt: "короткий шаблон"}
	if err := validateLocalSettings(ok, []string{"Q4_K_M"}); err != nil {
		t.Fatal(err)
	}
	bad := ok
	bad.Quant = "Q8_0"
	if err := validateLocalSettings(bad, []string{"Q4_K_M"}); err == nil {
		t.Fatal("чужое квантование прошло")
	}
}

func TestCookbookSampling(t *testing.T) {
	s := CookbookSampling()
	if s.Temperature == nil || *s.Temperature != tuneTempAfter || s.MaxTokens != tuneMaxAfter || s.NumCtx != tuneCtxAfter || s.ReasoningEffort != "none" {
		t.Fatalf("%+v", s)
	}
}
