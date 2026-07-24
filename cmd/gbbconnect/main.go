package main

import (
	"context"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	handled, err := runAsPlatformService(version)
	if handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

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
