package s1

import (
	"encoding/json"

	"github.com/tosh17/deepseek-service/internal/mcpsrv"
)

// Tools — команды s1. Модель выбирает инструмент, сырой shell ей не отдаётся.
func Tools(run Runner) []mcpsrv.Tool {
	if run == nil {
		run = Exec{}
	}
	return []mcpsrv.Tool{
		{
			Name:        "list_services",
			Description: "Список контейнеров на s1: имя, статус, порты. Вызывай, когда нужно понять, что сейчас запущено.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			Handle: func(json.RawMessage) (string, error) {
				return run.Run(bg(), ListServices())
			},
		},
		{
			Name:        "service_logs",
			Description: "Хвост логов одного контейнера на s1. name — имя из list_services, например gitlab или ollama.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"tail":{"type":"integer"}},"required":["name"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Name string `json:"name"`
					Tail int    `json:"tail"`
				}
				_ = json.Unmarshal(args, &in)
				argv, err := Logs(in.Name, in.Tail)
				if err != nil {
					return "", err
				}
				return run.Run(bg(), argv)
			},
		},
		{
			Name:        "service_status",
			Description: "Состояние контейнера: running и health.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(args, &in)
				argv, err := Status(in.Name)
				if err != nil {
					return "", err
				}
				return run.Run(bg(), argv)
			},
		},
	}
}
