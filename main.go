package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"chinspector/internal/config"
	"chinspector/internal/ui"
)

func main() {
	dsn, err := config.Parse(os.Args[1:], os.Getenv("CLICKHOUSE_URL"), os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := ui.Run(dsn); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
