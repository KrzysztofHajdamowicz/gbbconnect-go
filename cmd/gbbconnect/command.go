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
	return newRootCommandWithDependencies(
		buildVersion,
		defaultDiscoveryDependencies(),
		defaultRunDependencies(),
	)
}

func newRootCommandWithDiscovery(
	buildVersion string,
	discoveryDependencies discoveryDependencies,
) *cobra.Command {
	return newRootCommandWithDependencies(
		buildVersion,
		discoveryDependencies,
		defaultRunDependencies(),
	)
}

func newRootCommandWithDependencies(
	buildVersion string,
	discoveryDependencies discoveryDependencies,
	runDependencies runDependencies,
) *cobra.Command {
	var options globalOptions
	root := &cobra.Command{
		Use:           "gbbconnect",
		Short:         "MQTT-to-Modbus bridge for GbbOptimizer",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApplication(cmd, buildVersion, &options, runDependencies)
		},
	}
	root.PersistentFlags().StringVar(
		&options.configPath,
		"config",
		"",
		"path to gbbconnect.yaml or options.json",
	)
	root.PersistentFlags().StringVar(
		&options.stateDir,
		"state-dir",
		"",
		"directory for persistent runtime state",
	)
	root.PersistentFlags().StringVar(
		&options.logLevel,
		"log-level",
		"",
		"override logging level (error, warn, info, debug)",
	)
	root.PersistentFlags().BoolVar(
		&options.dev,
		"dev",
		false,
		"enable development runtime timings and debug behaviour",
	)

	root.AddCommand(
		newRunCommand(buildVersion, &options, runDependencies),
		newDiscoverCommand(discoveryDependencies),
		newImportXMLCommand(),
		newConfigCommand(&options.configPath),
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
	addPlatformCommands(root)

	return root
}

func newConfigCommand(configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate gbbconnect configuration",
	}

	validateCommand := &cobra.Command{
		Use:   "validate",
		Short: "Validate a YAML or JSON configuration file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == nil || *configPath == "" {
				return fmt.Errorf("--config is required")
			}

			loaded, err := config.Load(config.LoadOptions{Path: *configPath})
			if err != nil {
				return err
			}
			if err := config.ValidateSchemaFile(*configPath); err != nil {
				return err
			}
			if err := config.ValidateSchema(loaded); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Configuration %s is valid\n", *configPath)
			return err
		},
	}
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
