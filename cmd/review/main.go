package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/tosh17/deepseek-service/internal/config"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/rag"
	"github.com/tosh17/deepseek-service/internal/review"
)

func main() {
	root := flag.String("root", ".", "корень репозитория")
	base := flag.String("base", os.Getenv("CI_MERGE_REQUEST_DIFF_BASE_SHA"), "база diff, в CI это CI_MERGE_REQUEST_DIFF_BASE_SHA")
	head := flag.String("head", headDefault(), "голова diff")
	outPath := flag.String("out", "review.md", "куда записать ревью")
	flag.Parse()

	diff, err := review.GitDiff(*root, *base, *head)
	if err != nil {
		log.Fatalf("diff: %v", err)
	}
	if strings.TrimSpace(diff) == "" {
		const empty = "## Потенциальные баги\nнет\n\n## Архитектурные проблемы\nнет\n\n## Рекомендации\nDiff пустой, смотреть нечего.\n"
		if err := os.WriteFile(*outPath, []byte(empty), 0o644); err != nil {
			log.Fatalf("write %s: %v", *outPath, err)
		}
		fmt.Fprint(os.Stdout, empty)
		log.Printf("review: diff empty, out=%s", *outPath)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	ollamaURL := env("RAG_OLLAMA_URL", "http://10.10.1.101:11434")
	model := env("RAG_EMBED_MODEL", "nomic-embed-text")
	index := rag.New(rag.Options{
		Root:        *root,
		CachePath:   env("RAG_REVIEW_CACHE", "data/rag/review-index.json"),
		Model:       model,
		OllamaURL:   ollamaURL,
		Embedder:    rag.NewOllama(ollamaURL, model),
		TopK:        6,
		IncludeCode: true,
	})
	if err := index.Ensure(ctx); err != nil {
		log.Fatalf("rag: %v", err)
	}
	st := index.Status()
	log.Printf("rag: mode=%s chunks=%d files=%d embed_error=%s", st.Mode, st.Chunks, len(st.Files), st.EmbedError)
	guide := &review.Reviewer{Index: index, Complete: completer(), TopK: 6}
	result, err := guide.Run(ctx, diff)
	if err != nil {
		log.Fatalf("review: %v", err)
	}
	if err := os.WriteFile(*outPath, []byte(result.Text+"\n"), 0o644); err != nil {
		log.Fatalf("write %s: %v", *outPath, err)
	}
	fmt.Fprintln(os.Stdout, result.Text)
	if result.ModelError != "" {
		log.Printf("model: %s", result.ModelError)
	}
	log.Printf("review: files=%d mode=%s model=%v sources=%d out=%s", len(result.Files), result.Mode, result.Model, len(result.Sources), *outPath)

	if err := post(ctx, result.Text); err != nil {
		log.Printf("gitlab: %v", err)
	}
}

func headDefault() string {
	if sha := os.Getenv("CI_COMMIT_SHA"); sha != "" {
		return sha
	}
	return "HEAD"
}

func completer() review.Completer {
	url := os.Getenv("REVIEW_API_URL")
	model := os.Getenv("REVIEW_MODEL")
	key := os.Getenv("REVIEW_API_KEY")
	if url == "" {
		url = os.Getenv("DEEPSEEK_API_URL")
	}
	if model == "" {
		model = os.Getenv("DEEPSEEK_MODEL")
	}
	if key == "" {
		key = os.Getenv("DEEPSEEK_API_KEY")
	}
	if url == "" || key == "" {
		return nil
	}
	if model == "" {
		model = "deepseek-v4-flash"
	}
	client := deepseek.NewClient(key, model, config.NormalizeChatURL(url))
	return func(ctx context.Context, system, user string) (string, error) {
		res, err := client.Chat(ctx, []deepseek.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		})
		if err != nil {
			return "", err
		}
		return res.Reply, nil
	}
}

func post(ctx context.Context, body string) error {
	project := os.Getenv("CI_PROJECT_ID")
	iid := os.Getenv("CI_MERGE_REQUEST_IID")
	if project == "" || iid == "" {
		return nil
	}
	api := os.Getenv("CI_API_V4_URL")
	if api == "" {
		api = env("GITLAB_API_URL", "http://10.10.1.101:8929/api/v4")
	}
	token := os.Getenv("REVIEW_GITLAB_TOKEN")
	header := "PRIVATE-TOKEN"
	if token == "" {
		token = os.Getenv("CI_JOB_TOKEN")
		header = "JOB-TOKEN"
	}
	if token == "" {
		return fmt.Errorf("нет токена, комментарий не отправлен")
	}
	if err := review.PostNote(ctx, api, project, iid, token, header, body); err != nil {
		return err
	}
	log.Printf("gitlab: комментарий добавлен к !%s", iid)
	return nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
