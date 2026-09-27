package mcpsrv

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxSearchHits = 5
	maxPageRunes  = 4500
	searchUA      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
	imageSearchUA = "Mozilla/5.0"
)

// SearchTools — поиск в интернете и чтение страницы по ссылке.
func SearchTools(client *http.Client) []Tool {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return []Tool{
		{
			Name:        "web_search",
			Description: "Ищет только английские страницы. Запрос q пиши по-английски. Потом открой ссылку через web_fetch и переведи выдержку через translate_en_ru.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"English search query"}},"required":["q"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Q string `json:"q"`
				}
				_ = json.Unmarshal(args, &in)
				q := strings.TrimSpace(in.Q)
				if q == "" {
					return "", fmt.Errorf("нужен q")
				}
				hits, err := searchWeb(client, q)
				if err != nil {
					return "", err
				}
				raw, err := json.Marshal(map[string]any{"q": q, "results": hits})
				if err != nil {
					return "", err
				}
				return string(raw), nil
			},
		},
		{
			Name:        "web_fetch",
			Description: "Открывает одну английскую http(s) ссылку из web_search и возвращает текст страницы. Русские и китайские сайты не открывает.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"Ссылка из результатов поиска"}},"required":["url"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					URL string `json:"url"`
				}
				_ = json.Unmarshal(args, &in)
				page, err := fetchPage(client, strings.TrimSpace(in.URL))
				if err != nil {
					return "", err
				}
				raw, err := json.Marshal(page)
				if err != nil {
					return "", err
				}
				return string(raw), nil
			},
		},
		{
			Name:        "web_image",
			Description: "Ищет фото блюда и возвращает image_url. q — английское название блюда. В ответе пользователю поставь image_url отдельной строкой сразу под этим блюдом, без HTML.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"English dish name"}},"required":["q"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Q string `json:"q"`
				}
				_ = json.Unmarshal(args, &in)
				q := strings.TrimSpace(in.Q)
				if q == "" {
					return "", fmt.Errorf("нужен q")
				}
				hit, path, err := searchImages(client, q)
				if err != nil {
					return "", err
				}
				raw, err := json.Marshal(map[string]any{
					"q":          q,
					"title":      hit.Title,
					"source_url": hit.Page,
					"saved_path": path,
					"image_url":  "/media/" + filepath.Base(path),
				})
				if err != nil {
					return "", err
				}
				return string(raw), nil
			},
		},
	}
}

type searchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

func searchWeb(client *http.Client, q string) ([]searchHit, error) {
	if foreignScript(q) {
		return nil, fmt.Errorf("запрос web_search только на английском")
	}
	u := "https://html.duckduckgo.com/html/?kl=us-en&q=" + url.QueryEscape(q)
	body, err := httpGet(client, u)
	if err != nil {
		return nil, err
	}
	hits := englishHits(parseDuckDuckGo(string(body)))
	if len(hits) == 0 {
		return nil, fmt.Errorf("английских страниц не нашлось")
	}
	if len(hits) > maxSearchHits {
		hits = hits[:maxSearchHits]
	}
	return hits, nil
}

var (
	resultLink = regexp.MustCompile(`(?is)<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	resultSnip = regexp.MustCompile(`(?is)<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>|<td[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</td>`)
)

func parseDuckDuckGo(html string) []searchHit {
	links := resultLink.FindAllStringSubmatch(html, -1)
	snips := resultSnip.FindAllStringSubmatch(html, -1)
	var hits []searchHit
	for i, m := range links {
		rawURL := unescapeDuck(m[1])
		title := cleanText(m[2])
		if rawURL == "" || title == "" {
			continue
		}
		hit := searchHit{Title: title, URL: rawURL}
		if i < len(snips) {
			snip := snips[i][1]
			if snip == "" {
				snip = snips[i][2]
			}
			hit.Snippet = trimRunes(cleanText(snip), 220)
		}
		hits = append(hits, hit)
	}
	return hits
}

func unescapeDuck(href string) string {
	href = strings.TrimSpace(htmlUnescape(href))
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		if decoded, err := url.QueryUnescape(uddg); err == nil {
			return decoded
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func fetchPage(client *http.Client, raw string) (map[string]string, error) {
	if err := publicHTTP(raw); err != nil {
		return nil, err
	}
	if err := englishHost(raw); err != nil {
		return nil, err
	}
	body, err := httpGet(client, raw)
	if err != nil {
		return nil, err
	}
	text := trimRunes(cleanText(stripTags(string(body))), maxPageRunes)
	if text == "" {
		return nil, fmt.Errorf("на странице нет текста")
	}
	return map[string]string{"url": raw, "text": text}, nil
}

func httpGet(client *http.Client, raw string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", searchUA)
	return httpDo(client, req)
}

func httpGetUA(client *http.Client, raw, ua string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	return httpDo(client, req)
}

func httpDo(client *http.Client, req *http.Request) ([]byte, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusAccepted || res.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return body, nil
}

func englishHits(hits []searchHit) []searchHit {
	var out []searchHit
	for _, hit := range hits {
		if foreignScript(hit.Title + " " + hit.Snippet) {
			continue
		}
		if englishHost(hit.URL) != nil {
			continue
		}
		out = append(out, hit)
	}
	return out
}

func englishHost(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("нужна английская страница")
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	for _, suf := range []string{".ru", ".su", ".cn", ".tw", ".jp", ".kr", ".ua", ".by", ".kz", ".uz"} {
		if strings.HasSuffix(host, suf) {
			return fmt.Errorf("беру только английские страницы")
		}
	}
	if strings.Contains(path, "/ru/") || strings.Contains(path, "/zh/") || strings.Contains(path, "/cn/") {
		return fmt.Errorf("беру только английские страницы")
	}
	return nil
}

func foreignScript(s string) bool {
	for _, r := range s {
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
		if r >= 0x3040 && r <= 0x30FF {
			return true
		}
		if r >= 0x3400 && r <= 0x9FFF {
			return true
		}
		if r >= 0xAC00 && r <= 0xD7AF {
			return true
		}
	}
	return false
}

func publicHTTP(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("нужна публичная http(s) ссылка")
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("локальные адреса не открываю")
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicIP(ip) {
		return fmt.Errorf("локальные адреса не открываю")
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified()
}

var tagRE = regexp.MustCompile(`(?is)<script[\s\S]*?</script>|<style[\s\S]*?</style>|<[^>]+>`)

func stripTags(s string) string {
	return tagRE.ReplaceAllString(s, " ")
}

func cleanText(s string) string {
	s = htmlUnescape(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func htmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&nbsp;", " ",
	)
	return r.Replace(s)
}

func trimRunes(s string, n int) string {
	if utfLen(s) <= n {
		return s
	}
	out := make([]rune, 0, n)
	for _, r := range s {
		if len(out) == n {
			break
		}
		out = append(out, r)
	}
	return string(out) + "…"
}

func utfLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
