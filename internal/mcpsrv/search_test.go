package mcpsrv

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDuckDuckGo(t *testing.T) {
	html := `<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fdocs&amp;rut=1">Example Docs</a>
<a class="result__snippet" href="x">Short snippet</a>`
	hits := parseDuckDuckGo(html)
	if len(hits) != 1 {
		t.Fatalf("hits: %+v", hits)
	}
	if hits[0].URL != "https://example.com/docs" || hits[0].Title != "Example Docs" {
		t.Fatalf("hit: %+v", hits[0])
	}
	if !strings.Contains(hits[0].Snippet, "snippet") {
		t.Fatalf("snippet: %+v", hits[0])
	}
}

func TestParseDDGImages(t *testing.T) {
	body := []byte(`{"results":[
		{"title":"Tiny","image":"https://cdn.example/tiny.jpg","url":"https://example.com/t","width":16,"height":16},
		{"title":"Tower","image":"https://cdn.example/tower.jpg","thumbnail":"https://cdn.example/thumb.jpg","url":"https://example.com/a","width":800,"height":600}
	]}`)
	hits := parseDDGImages(body)
	if len(hits) != 1 || hits[0].Title != "Tower" || hits[0].Image != "https://cdn.example/tower.jpg" {
		t.Fatalf("hits: %+v", hits)
	}
}

func TestImageExt(t *testing.T) {
	if imageExt([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0}) != ".png" {
		t.Fatal("png")
	}
	if imageExt([]byte{0xFF, 0xD8, 0xFF, 0xD9}) != ".jpg" {
		t.Fatal("jpg")
	}
	if imageExt([]byte("<html>")) != "" {
		t.Fatal("html accepted")
	}
}

func TestPublicResolvedRejectsLocalName(t *testing.T) {
	if err := publicResolved("http://127.0.0.1/a.jpg"); err == nil {
		t.Fatal("local image allowed")
	}
}

func TestEnglishOnly(t *testing.T) {
	if !foreignScript("рецепт") || !foreignScript("男性") {
		t.Fatal("script")
	}
	if foreignScript("oatmeal with egg") {
		t.Fatal("english flagged")
	}
	hits := englishHits([]searchHit{
		{Title: "Oatmeal", URL: "https://example.com/oats"},
		{Title: "Овсянка", URL: "https://example.com/ru"},
		{Title: "Oatmeal", URL: "https://eda.ru/recipe"},
	})
	if len(hits) != 1 || hits[0].URL != "https://example.com/oats" {
		t.Fatalf("hits: %+v", hits)
	}
	if err := englishHost("https://yummybook.ru/recept"); err == nil {
		t.Fatal("ru host allowed")
	}
}

func TestPublicHTTPRejectsLocal(t *testing.T) {
	if err := publicHTTP("http://127.0.0.1/secret"); err == nil {
		t.Fatal("local url allowed")
	}
	if err := publicHTTP("https://example.com/a"); err != nil {
		t.Fatal(err)
	}
}

func TestFlowPNG(t *testing.T) {
	path, err := drawFlow(flowInput{
		Title: "Ход",
		Steps: []flowStep{
			{Server: "поиск", Tool: "web_search", Detail: "golang mcp"},
			{Server: "перевод", Tool: "translate_en_ru", Detail: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.Base(path), "flow-") || !strings.HasSuffix(path, ".png") {
		t.Fatalf("path: %s", path)
	}
}
