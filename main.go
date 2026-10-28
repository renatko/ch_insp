package main

import (
	"fmt"
	"os"

	"chinspector/internal/ui"
)

func main() {
	if err := ui.Run(os.Getenv("CLICKHOUSE_URL")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
