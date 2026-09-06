package deepseek

// ModelTier — уровень модели для сравнения (день 5).
type ModelTier string

const (
	TierWeak   ModelTier = "weak"
	TierMedium ModelTier = "medium"
	TierStrong ModelTier = "strong"
)

// ModelSpec описывает конфигурацию запроса к DeepSeek.
type ModelSpec struct {
	ID          ModelTier `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Model       string    `json:"model"`
	Thinking    string    `json:"thinking"` // enabled | disabled
	Effort      string    `json:"effort,omitempty"`
}

// PricingUSD — цена за 1M токенов (off-peak, ориентир для оценки стоимости).
type PricingUSD struct {
	InputCacheHit  float64
	InputCacheMiss float64
	Output         float64
}

var modelPricing = map[string]PricingUSD{
	"deepseek-v4-flash": {
		InputCacheHit:  0.007,
		InputCacheMiss: 0.22,
		Output:         0.66,
	},
	"deepseek-v4-pro": {
		InputCacheHit:  0.022,
		InputCacheMiss: 0.66,
		Output:         1.98,
	},
}

// TierSpecs — три уровня: Flash без thinking, Flash с thinking, Pro с thinking.
func TierSpecs() []ModelSpec {
	return []ModelSpec{
		{
			ID:          TierWeak,
			Title:       "Слабая · Flash",
			Description: "deepseek-v4-flash, без thinking — быстро и дёшево",
			Model:       "deepseek-v4-flash",
			Thinking:    "disabled",
		},
		{
			ID:          TierMedium,
			Title:       "Средняя · Flash Think",
			Description: "deepseek-v4-flash + thinking — рассуждение на лёгкой модели",
			Model:       "deepseek-v4-flash",
			Thinking:    "enabled",
			Effort:      "high",
		},
		{
			ID:          TierStrong,
			Title:       "Сильная · Pro",
			Description: "deepseek-v4-pro + thinking — максимальное качество",
			Model:       "deepseek-v4-pro",
			Thinking:    "enabled",
			Effort:      "high",
		},
	}
}

func EstimateCostUSD(model string, usage Usage) float64 {
	p, ok := modelPricing[model]
	if !ok {
		p = modelPricing["deepseek-v4-flash"]
	}

	hit := usage.PromptCacheHitTokens
	miss := usage.PromptCacheMissTokens
	if hit == 0 && miss == 0 && usage.PromptTokens > 0 {
		miss = usage.PromptTokens
	}

	return float64(hit)/1_000_000*p.InputCacheHit +
		float64(miss)/1_000_000*p.InputCacheMiss +
		float64(usage.CompletionTokens)/1_000_000*p.Output
}
