package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if err := newRootCommand(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
