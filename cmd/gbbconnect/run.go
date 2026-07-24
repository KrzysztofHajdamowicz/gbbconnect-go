package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/state"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/supervisor"
	"github.com/spf13/cobra"
)

type globalOptions struct {
	configPath string
	stateDir   string
	logLevel   string
	dev        bool
}

type serviceOptions struct {
	Version  string
	Config   config.Config
	StateDir string
	LogDir   string
	Logger   *logbuf.Runtime
}

type runDependencies struct {
	resolveConfig   func(config.LoadOptions) (string, error)
	loadConfig      func(config.LoadOptions) (config.Config, error)
	resolveStateDir func(state.DirOptions) (string, error)
	newLogger       func(logbuf.Options) (*logbuf.Runtime, error)
	runService      func(context.Context, serviceOptions) error
}

func defaultRunDependencies() runDependencies {
	return runDependencies{
		resolveConfig:   config.ResolvePath,
		loadConfig:      config.Load,
		resolveStateDir: state.ResolveDir,
		newLogger:       logbuf.New,
		runService:      runSupervisor,
	}
}

func newRunCommand(
	buildVersion string,
	options *globalOptions,
	dependencies runDependencies,
) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the MQTT-to-Modbus bridge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApplication(
				cmd,
				buildVersion,
				options,
				dependencies,
			)
		},
	}
}

func runApplication(
	command *cobra.Command,
	buildVersion string,
	options *globalOptions,
	dependencies runDependencies,
) (result error) {
	if options == nil {
		return errors.New("global options are required")
	}
	if dependencies.resolveConfig == nil ||
		dependencies.loadConfig == nil ||
		dependencies.resolveStateDir == nil ||
		dependencies.newLogger == nil ||
		dependencies.runService == nil {
		return errors.New("run dependencies are incomplete")
	}

	configPath, err := dependencies.resolveConfig(config.LoadOptions{
		Path: options.configPath,
	})
	if err != nil {
		var missing *config.MissingConfigError
		if errors.As(err, &missing) {
			return err
		}
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"%w\n\nExample gbbconnect.yaml:\n%s",
				err,
				config.Sample(),
			)
		}
		return err
	}

	loaded, err := dependencies.loadConfig(config.LoadOptions{Path: configPath})
	if err != nil {
		return err
	}
	if command.Root().PersistentFlags().Changed("dev") {
		loaded.Runtime.Debug = options.dev
	}
	if options.logLevel != "" {
		loaded.Logging.Level = config.LogLevel(options.logLevel)
		if _, err := configuredLogLevel(loaded.Logging.Level); err != nil {
			return err
		}
	}

	stateDir, err := dependencies.resolveStateDir(state.DirOptions{
		Path: options.stateDir,
	})
	if err != nil {
		return err
	}
	logDir := loaded.Logging.Directory
	if logDir == "" {
		logDir = filepath.Join(stateDir, "logs")
	}

	level, err := configuredLogLevel(loaded.Logging.Level)
	if err != nil {
		return err
	}
	logger, err := dependencies.newLogger(logbuf.Options{
		Level:  level,
		Format: logbuf.FormatText,
		Output: command.OutOrStdout(),
		LogDir: logDir,
	})
	if err != nil {
		return fmt.Errorf("initialize logging: %w", err)
	}
	defer func() {
		result = errors.Join(result, logger.Close())
	}()
	logger.SetDriverTrace(
		loaded.Logging.DriverTrace,
		loaded.Logging.DriverTraceRaw,
	)

	if _, err := fmt.Fprintf(
		command.OutOrStdout(),
		"gbbconnect %s starting: config=%s state-dir=%s plants=%d\n",
		buildVersion,
		configPath,
		stateDir,
		len(loaded.Plants),
	); err != nil {
		return err
	}

	return dependencies.runService(command.Context(), serviceOptions{
		Version:  buildVersion,
		Config:   loaded,
		StateDir: stateDir,
		LogDir:   logDir,
		Logger:   logger,
	})
}

func configuredLogLevel(level config.LogLevel) (logbuf.Level, error) {
	switch level {
	case config.LogLevelError:
		return logbuf.LevelError, nil
	case config.LogLevelWarn:
		return logbuf.LevelWarn, nil
	case config.LogLevelInfo:
		return logbuf.LevelInfo, nil
	case config.LogLevelDebug:
		return logbuf.LevelDebug, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", level)
	}
}

func runSupervisor(ctx context.Context, options serviceOptions) error {
	service, err := supervisor.New(options.Config, supervisor.Options{
		Version:  options.Version,
		StateDir: options.StateDir,
		LogDir:   options.LogDir,
		Logger:   options.Logger,
	})
	if err != nil {
		return fmt.Errorf("initialize supervisor: %w", err)
	}
	return service.Run(ctx)
}
