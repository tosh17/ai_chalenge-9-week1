package main

import (
	"fmt"
	"os"

	"github.com/tosh17/deepseek-service/internal/mcpsrv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mcp translate|search|diagram|project")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "translate":
		mcpsrv.Serve("translate", mcpsrv.TranslateTools(nil))
	case "search":
		mcpsrv.Serve("search", mcpsrv.SearchTools(nil))
	case "diagram":
		mcpsrv.Serve("diagram", mcpsrv.DiagramTools())
	case "project":
		root := os.Getenv("PROJECT_ROOT")
		if root == "" {
			root, _ = os.Getwd()
		}
		mcpsrv.Serve("project", mcpsrv.ProjectTools(root))
	default:
		fmt.Fprintf(os.Stderr, "unknown mcp %q\n", os.Args[1])
		os.Exit(2)
	}
}
