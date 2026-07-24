package main

import (
	"fmt"
	"os"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config/xmlimport"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
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
		newImportXMLCommand(),
		newConfigCommand(),
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

func newConfigCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate gbbconnect configuration",
	}

	var configPath string
	validateCommand := &cobra.Command{
		Use:   "validate",
		Short: "Validate a YAML or JSON configuration file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}

			loaded, err := config.Load(config.LoadOptions{Path: configPath})
			if err != nil {
				return err
			}
			if err := config.ValidateSchemaFile(configPath); err != nil {
				return err
			}
			if err := config.ValidateSchema(loaded); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Configuration %s is valid\n", configPath)
			return err
		},
	}
	validateCommand.Flags().StringVar(&configPath, "config", "", "path to gbbconnect.yaml or options.json")
	command.AddCommand(validateCommand)
	return command
}

func newImportXMLCommand() *cobra.Command {
	var inputPath string
	var outputPath string

	command := &cobra.Command{
		Use:   "import-xml",
		Short: "Import a legacy GbbConnect2 Parameters.xml file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if inputPath == "" {
				return fmt.Errorf("--in is required")
			}
			if outputPath == "" {
				return fmt.Errorf("--out is required")
			}

			input, err := os.Open(inputPath)
			if err != nil {
				return fmt.Errorf("open input XML: %w", err)
			}
			imported, warnings, importErr := xmlimport.Import(input)
			closeErr := input.Close()
			if importErr != nil {
				return importErr
			}
			if closeErr != nil {
				return fmt.Errorf("close input XML: %w", closeErr)
			}
			if err := config.Validate(imported); err != nil {
				return fmt.Errorf("validate imported configuration: %w", err)
			}

			data, err := yaml.Marshal(imported)
			if err != nil {
				return fmt.Errorf("encode imported configuration: %w", err)
			}
			if err := os.WriteFile(outputPath, data, 0o600); err != nil {
				return fmt.Errorf("write output YAML: %w", err)
			}

			for _, warning := range warnings {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Imported %s to %s\n", inputPath, outputPath)
			return err
		},
	}
	command.Flags().StringVar(&inputPath, "in", "", "path to Parameters.xml")
	command.Flags().StringVar(&outputPath, "out", "", "path for gbbconnect.yaml")
	return command
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
