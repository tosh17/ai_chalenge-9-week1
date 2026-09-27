package mcpsrv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxImageBytes = 4 << 20

type imageCandidate struct {
	Title     string
	Image     string
	Thumbnail string
	Page      string
	Width     int
	Height    int
}

func searchImages(client *http.Client, q string) (imageCandidate, string, error) {
	hits, err := duckImages(client, q)
	if err != nil {
		return imageCandidate{}, "", err
	}
	saved, picked, err := saveFirstImage(client, hits)
	if err != nil {
		return imageCandidate{}, "", err
	}
	return picked, saved, nil
}

func duckImages(client *http.Client, q string) ([]imageCandidate, error) {
	home := "https://duckduckgo.com/?q=" + url.QueryEscape(q)
	page, err := httpGetUA(client, home, imageSearchUA)
	if err != nil {
		return nil, err
	}
	vqd := vqdToken(string(page))
	if vqd == "" {
		return nil, fmt.Errorf("поиск картинок не ответил")
	}
	api := "https://duckduckgo.com/i.js?l=ru-ru&o=json&q=" + url.QueryEscape(q) + "&vqd=" + url.QueryEscape(vqd) + "&f=,,,,,&p=1"
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", imageSearchUA)
	req.Header.Set("Referer", "https://duckduckgo.com/")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	hits := parseDDGImages(body)
	if len(hits) == 0 {
		return nil, fmt.Errorf("картинок не нашлось")
	}
	return hits, nil
}

var vqdRE = regexp.MustCompile(`vqd="([^"]+)"`)

func vqdToken(html string) string {
	m := vqdRE.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func parseDDGImages(body []byte) []imageCandidate {
	var payload struct {
		Results []struct {
			Title     string `json:"title"`
			Image     string `json:"image"`
			Thumbnail string `json:"thumbnail"`
			URL       string `json:"url"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	var hits []imageCandidate
	for _, row := range payload.Results {
		if row.Width > 0 && row.Height > 0 && (row.Width < 240 || row.Height < 180) {
			continue
		}
		if strings.TrimSpace(row.Image) == "" && strings.TrimSpace(row.Thumbnail) == "" {
			continue
		}
		hits = append(hits, imageCandidate{
			Title:     cleanText(row.Title),
			Image:     strings.TrimSpace(row.Image),
			Thumbnail: strings.TrimSpace(row.Thumbnail),
			Page:      strings.TrimSpace(row.URL),
			Width:     row.Width,
			Height:    row.Height,
		})
		if len(hits) == 6 {
			break
		}
	}
	return hits
}

func saveFirstImage(client *http.Client, hits []imageCandidate) (string, imageCandidate, error) {
	var last error
	for _, hit := range hits {
		for _, raw := range []string{hit.Image, hit.Thumbnail} {
			if raw == "" {
				continue
			}
			path, err := downloadImage(client, raw)
			if err != nil {
				last = err
				continue
			}
			return path, hit, nil
		}
	}
	if last == nil {
		last = fmt.Errorf("картинку скачать не удалось")
	}
	return "", imageCandidate{}, last
}

func downloadImage(client *http.Client, raw string) (string, error) {
	if err := publicResolved(raw); err != nil {
		return "", err
	}
	dl := &http.Client{
		Timeout:   20 * time.Second,
		Transport: client.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("слишком много редиректов")
			}
			return publicResolved(req.URL.String())
		},
	}
	if client != nil && client.Timeout > 0 && client.Timeout < dl.Timeout {
		dl.Timeout = client.Timeout
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", searchUA)
	res, err := dl.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
	if err != nil {
		return "", err
	}
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if len(body) > maxImageBytes {
		return "", fmt.Errorf("картинка слишком большая")
	}
	ext := imageExt(body)
	if ext == "" {
		return "", fmt.Errorf("это не картинка")
	}
	dir := filepath.Join("data", "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("img-%d%s", time.Now().UnixNano(), ext))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

func publicResolved(raw string) error {
	if err := publicHTTP(raw); err != nil {
		return err
	}
	host := hostnameOf(raw)
	if host == "" || net.ParseIP(host) != nil {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("не открываю %s", host)
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return fmt.Errorf("локальные адреса не открываю")
		}
	}
	return nil
}

func hostnameOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func imageExt(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return ".png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return ".jpg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return ".gif"
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return ".webp"
	default:
		return ""
	}
}
