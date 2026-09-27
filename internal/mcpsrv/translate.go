package mcpsrv

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const translateChunk = 450

// TranslateTools — сервер перевода en → ru.
func TranslateTools(client *http.Client) []Tool {
	if client == nil {
		client = http.DefaultClient
	}
	return []Tool{{
		Name:        "translate_en_ru",
		Description: "Переводит английский текст на русский. Вызывай после web_fetch, если страница на английском. На вход — кусок текста, не URL.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"Английский текст"}},"required":["text"]}`),
		Handle: func(args json.RawMessage) (string, error) {
			var in struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &in)
			text := strings.TrimSpace(in.Text)
			if text == "" {
				return "", fmt.Errorf("нужен text")
			}
			return translateENRU(client, text)
		},
	}}
}

func translateENRU(client *http.Client, text string) (string, error) {
	var parts []string
	for _, chunk := range splitRunes(text, translateChunk) {
		out, err := translateChunkENRU(client, chunk)
		if err != nil {
			return "", err
		}
		parts = append(parts, out)
	}
	return strings.TrimSpace(strings.Join(parts, " ")), nil
}

func translateChunkENRU(client *http.Client, text string) (string, error) {
	q := url.Values{}
	q.Set("q", text)
	q.Set("langpair", "en|ru")
	req, err := http.NewRequest(http.MethodGet, "https://api.mymemory.translated.net/get?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "week1-translate/day20")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("перевод: HTTP %d", res.StatusCode)
	}
	var payload struct {
		ResponseData struct {
			TranslatedText string `json:"translatedText"`
		} `json:"responseData"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	out := strings.TrimSpace(payload.ResponseData.TranslatedText)
	if out == "" {
		return "", fmt.Errorf("перевод пустой")
	}
	return out, nil
}

func splitRunes(text string, n int) []string {
	if utf8.RuneCountInString(text) <= n {
		return []string{text}
	}
	var parts []string
	var b strings.Builder
	count := 0
	for _, r := range text {
		b.WriteRune(r)
		count++
		if count >= n && (r == ' ' || r == '\n' || r == '.') {
			parts = append(parts, strings.TrimSpace(b.String()))
			b.Reset()
			count = 0
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		parts = append(parts, strings.TrimSpace(b.String()))
	}
	return parts
}
