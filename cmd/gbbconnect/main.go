package main

import (
	"context"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	ctx, stopSignals := newShutdownContext(
		context.Background(),
		defaultShutdownSignalDependencies(),
	)
	defer stopSignals()

	if err := newRootCommand(version).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
