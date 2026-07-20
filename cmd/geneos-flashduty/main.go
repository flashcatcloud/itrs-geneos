package main

import (
	"context"
	"fmt"
	"os"

	"github.com/flashcatcloud/itrs-geneos/internal/app"
)

func main() {
	if err := app.Run(context.Background(), os.Args[1:], app.Dependencies{}); err != nil {
		fmt.Fprintf(os.Stderr, "geneos-flashduty: %v\n", err)
		os.Exit(1)
	}
}
