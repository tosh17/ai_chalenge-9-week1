package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tosh17/deepseek-service/internal/dock"
	"github.com/tosh17/deepseek-service/internal/files"
	"github.com/tosh17/deepseek-service/internal/mcpsrv"
	"github.com/tosh17/deepseek-service/internal/s1"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "s1" {
		fmt.Fprintln(os.Stderr, "usage: mcp s1")
		os.Exit(2)
	}
	root := os.Getenv("OPS_ROOT")
	if root == "" {
		root = filepath.Join("ops", "configs")
	}
	stacks := os.Getenv("OPS_STACKS")
	if stacks == "" {
		stacks = filepath.Join("ops", "stacks")
	}
	tools := append(s1.Tools(s1.Exec{Host: os.Getenv("S1_HOST")}), files.Handlers(root)...)
	have := map[string]bool{}
	for _, tool := range tools {
		have[tool.Name] = true
	}
	for _, tool := range dock.Handlers(s1.Exec{Host: os.Getenv("S1_HOST")}, stacks) {
		if !have[tool.Name] {
			tools = append(tools, tool)
			have[tool.Name] = true
		}
	}
	mcpsrv.Serve("s1", tools)
}
