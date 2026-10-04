//go:debug x509usefallbackroots=1

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/tosh17/deepseek-service/internal/index"
)

func main() {
	corpus := flag.String("corpus", "corpus", "каталог с pdf, txt, md, go")
	out := flag.String("out", "index", "куда писать fixed.json, struct.json, compare.json")
	ollama := flag.String("ollama", "http://127.0.0.1:11434", "адрес Ollama")
	model := flag.String("model", "nomic-embed-text", "модель эмбеддингов")
	batch := flag.Int("batch", 8, "сколько чанков эмбеддить за один запрос")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	report, err := index.Run(ctx, index.Options{
		Corpus: *corpus,
		OutDir: *out,
		Ollama: *ollama,
		Model:  *model,
		Batch:  *batch,
	})
	if err != nil {
		log.Fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(report); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "индекс: %s/fixed.json и %s/struct.json\n", *out, *out)
}
