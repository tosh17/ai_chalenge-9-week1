package main

import (
	"fmt"
	"os"

	"github.com/tosh17/deepseek-service/internal/mcpsrv"
	"github.com/tosh17/deepseek-service/internal/s1"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "s1" {
		fmt.Fprintln(os.Stderr, "usage: mcp s1")
		os.Exit(2)
	}
	mcpsrv.Serve("s1", s1.Tools(s1.Exec{Host: os.Getenv("S1_HOST")}))
}
