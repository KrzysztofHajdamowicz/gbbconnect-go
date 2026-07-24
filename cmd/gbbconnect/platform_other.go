//go:build !windows

package main

import "github.com/spf13/cobra"

func runAsPlatformService(string) (bool, error) {
	return false, nil
}

func addPlatformCommands(*cobra.Command) {}
