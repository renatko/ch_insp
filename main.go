package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"ch_insp/internal/config"
	"ch_insp/internal/ui"
)

func main() {
	options, err := config.ParseOptions(os.Args[1:], os.Getenv("CLICKHOUSE_URL"), os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if options.SchemaPath != "" {
		err = ui.RunSchema(options.SchemaPath, options.Database)
	} else {
		err = ui.Run(options.DSN)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
