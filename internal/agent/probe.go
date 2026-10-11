package agent

import (
	"context"
	"fmt"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

type hostProber interface {
	Probe(ctx context.Context, shots int) (deepseek.ProbeReport, error)
}

// ProbeLocal гоняет проверку домашнего HTTP API: сеть, несколько запросов, контекст и rate limit.
func (a *Agent) ProbeLocal(ctx context.Context) (deepseek.ProbeReport, error) {
	b, ok := a.backends[ProviderLocal]
	if !ok || b.llm == nil {
		return deepseek.ProbeReport{}, fmt.Errorf("локальная модель не подключена")
	}
	prober, ok := b.llm.(hostProber)
	if !ok {
		return deepseek.ProbeReport{}, fmt.Errorf("локальный клиент не умеет проверку")
	}
	report, err := prober.Probe(ctx, 3)
	limit := b.contextLimit
	if limit <= 0 {
		limit = a.contextLimit
	}
	report.AppContextLimit = limit
	report.Summary = deepseek.FormatProbe(report)
	return report, err
}
