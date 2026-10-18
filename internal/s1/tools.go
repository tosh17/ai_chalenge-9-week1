package s1

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tosh17/deepseek-service/internal/mcpsrv"
)

// Tools — команды s1 и карточки CRM. Модель выбирает инструмент, сырой shell ей не отдаётся.
func Tools(run Runner, crmDir string) []mcpsrv.Tool {
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
		{
			Name:        "get_ticket",
			Description: "Тикет поддержки по id, например TCK-210. Бери сервис и факты отсюда.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(args, &in)
				return lookup(crmDir, "tickets.json", "tickets", "id", in.ID)
			},
		},
		{
			Name:        "get_user",
			Description: "Карточка пользователя CRM по id.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(args, &in)
				return lookup(crmDir, "users.json", "users", "id", in.ID)
			},
		},
		{
			Name:        "list_tickets",
			Description: "Все тикеты CRM.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			Handle: func(json.RawMessage) (string, error) {
				return readRaw(filepath.Join(crmDir, "tickets.json"))
			},
		},
	}
}

func lookup(dir, file, key, field, want string) (string, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		return "", fmt.Errorf("нужен %s", field)
	}
	raw, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return "", err
	}
	var doc map[string][]map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	for _, item := range doc[key] {
		got, _ := item[field].(string)
		if strings.EqualFold(got, want) {
			out, err := json.Marshal(item)
			if err != nil {
				return "", err
			}
			return string(out), nil
		}
	}
	return "", fmt.Errorf("%s %s не найден", key, want)
}

func readRaw(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
