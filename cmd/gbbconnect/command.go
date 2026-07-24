package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRootCommand(buildVersion string) *cobra.Command {
	root := &cobra.Command{
		Use:           "gbbconnect",
		Short:         "MQTT-to-Modbus bridge for GbbOptimizer",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	root.AddCommand(
		newStubCommand("run", "Run the MQTT-to-Modbus bridge"),
		newStubCommand("discover", "Discover supported inverter dongles"),
		&cobra.Command{
			Use:   "version",
			Short: "Print the gbbconnect version",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "gbbconnect version %s\n", buildVersion)
				return err
			},
		},
	)

	return root
}

func newStubCommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}
