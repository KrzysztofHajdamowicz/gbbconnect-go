//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceName        = "gbbconnect"
	windowsServiceDisplayName = "GbbConnect Go"
	windowsServiceDescription = "MQTT-to-Modbus bridge for GbbOptimizer"
	windowsServiceStopWait    = 40 * time.Second
)

type serviceEventLog interface {
	Info(uint32, string) error
	Warning(uint32, string) error
	Error(uint32, string) error
	Close() error
}

type windowsService struct {
	buildVersion string
	args         []string
	eventLog     serviceEventLog
}

type eventLogWriter struct {
	log serviceEventLog
	mu  sync.Mutex
}

func (writer *eventLogWriter) Write(data []byte) (int, error) {
	message := strings.TrimSpace(string(data))
	if message == "" {
		return len(data), nil
	}

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err := writer.log.Info(1, message); err != nil {
		return 0, err
	}
	return len(data), nil
}

func runAsPlatformService(buildVersion string) (handled bool, result error) {
	inService, err := svc.IsWindowsService()
	if err != nil {
		return true, fmt.Errorf("detect Windows service context: %w", err)
	}
	if !inService {
		return false, nil
	}

	var serviceLog serviceEventLog
	serviceLog, openErr := eventlog.Open(windowsServiceName)
	if openErr == nil {
		defer func() {
			result = errors.Join(result, serviceLog.Close())
		}()
	}

	handler := &windowsService{
		buildVersion: buildVersion,
		args:         append([]string(nil), os.Args[1:]...),
		eventLog:     serviceLog,
	}
	if err := svc.Run(windowsServiceName, handler); err != nil {
		return true, fmt.Errorf("run Windows service: %w", err)
	}
	return true, nil
}

func (service *windowsService) Execute(
	_ []string,
	requests <-chan svc.ChangeRequest,
	statuses chan<- svc.Status,
) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	statuses <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	command := newRootCommand(service.buildVersion)
	command.SetArgs(service.args)
	var output io.Writer = os.Stderr
	if service.eventLog != nil {
		output = &eventLogWriter{log: service.eventLog}
		_ = service.eventLog.Info(1, "gbbconnect service starting")
	}
	command.SetOut(output)
	command.SetErr(output)

	done := make(chan error, 1)
	go func() {
		done <- command.ExecuteContext(ctx)
	}()
	statuses <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-done:
			if err != nil {
				service.logError(fmt.Sprintf("gbbconnect service failed: %v", err))
				return false, 1
			}
			service.logInfo("gbbconnect service stopped")
			return false, 0
		case request, ok := <-requests:
			if !ok {
				cancel()
				return service.waitForStop(done, statuses)
			}
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{
					State:      svc.StopPending,
					CheckPoint: 1,
					WaitHint:   uint32(windowsServiceStopWait.Milliseconds()),
				}
				cancel()
				return service.waitForStop(done, statuses)
			default:
				service.logWarning(
					fmt.Sprintf("ignored Windows service control request %d", request.Cmd),
				)
			}
		}
	}
}

func (service *windowsService) waitForStop(
	done <-chan error,
	statuses chan<- svc.Status,
) (bool, uint32) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	checkpoint := uint32(1)

	for {
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				service.logError(fmt.Sprintf("gbbconnect shutdown failed: %v", err))
				return false, 1
			}
			service.logInfo("gbbconnect service stopped")
			return false, 0
		case <-ticker.C:
			checkpoint++
			statuses <- svc.Status{
				State:      svc.StopPending,
				CheckPoint: checkpoint,
				WaitHint:   uint32(windowsServiceStopWait.Milliseconds()),
			}
		}
	}
}

func (service *windowsService) logInfo(message string) {
	if service.eventLog != nil {
		_ = service.eventLog.Info(1, message)
	}
}

func (service *windowsService) logWarning(message string) {
	if service.eventLog != nil {
		_ = service.eventLog.Warning(1, message)
	}
}

func (service *windowsService) logError(message string) {
	if service.eventLog != nil {
		_ = service.eventLog.Error(1, message)
	}
}

func addPlatformCommands(root *cobra.Command) {
	serviceCommand := &cobra.Command{
		Use:   "service",
		Short: "Install or uninstall the Windows Service",
	}
	serviceCommand.AddCommand(
		&cobra.Command{
			Use:   "install",
			Short: "Install and enable the Windows Service",
			Args:  cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				configPath, stateDir := defaultWindowsServicePaths()
				if err := installWindowsService(configPath, stateDir); err != nil {
					return err
				}
				_, err := fmt.Fprintf(
					command.OutOrStdout(),
					"Installed %s. Place configuration at %s, then start it with: sc.exe start %s\n",
					windowsServiceDisplayName,
					configPath,
					windowsServiceName,
				)
				return err
			},
		},
		&cobra.Command{
			Use:   "uninstall",
			Short: "Uninstall the Windows Service",
			Args:  cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				if err := uninstallWindowsService(); err != nil {
					return err
				}
				_, err := fmt.Fprintf(
					command.OutOrStdout(),
					"Uninstalled %s; configuration and state were preserved.\n",
					windowsServiceDisplayName,
				)
				return err
			},
		},
	)
	root.AddCommand(serviceCommand)
}

func defaultWindowsServicePaths() (string, string) {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	base := filepath.Join(programData, "gbbconnect")
	return filepath.Join(base, "gbbconnect.yaml"), filepath.Join(base, "state")
}

func installWindowsService(configPath, stateDir string) (result error) {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("make executable path absolute: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o750); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows Service Control Manager: %w", err)
	}
	defer func() {
		result = errors.Join(result, manager.Disconnect())
	}()

	if existing, openErr := manager.OpenService(windowsServiceName); openErr == nil {
		_ = existing.Close()
		return fmt.Errorf("service %q is already installed", windowsServiceName)
	}

	created, err := manager.CreateService(
		windowsServiceName,
		executable,
		mgr.Config{
			DisplayName:  windowsServiceDisplayName,
			Description:  windowsServiceDescription,
			StartType:    mgr.StartAutomatic,
			ErrorControl: mgr.ErrorNormal,
			Dependencies: []string{"Tcpip"},
		},
		"--config", configPath,
		"--state-dir", stateDir,
		"run",
	)
	if err != nil {
		return fmt.Errorf("create Windows service: %w", err)
	}
	defer func() {
		result = errors.Join(result, created.Close())
	}()

	if err := eventlog.InstallAsEventCreate(
		windowsServiceName,
		eventlog.Error|eventlog.Warning|eventlog.Info,
	); err != nil {
		deleteErr := created.Delete()
		return errors.Join(
			fmt.Errorf("install Windows Event Log source: %w", err),
			deleteErr,
		)
	}
	return nil
}

func uninstallWindowsService() (result error) {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows Service Control Manager: %w", err)
	}
	defer func() {
		result = errors.Join(result, manager.Disconnect())
	}()

	service, err := manager.OpenService(windowsServiceName)
	if err != nil {
		return fmt.Errorf("open Windows service %q: %w", windowsServiceName, err)
	}
	defer func() {
		result = errors.Join(result, service.Close())
	}()
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query Windows service: %w", err)
	}
	if status.State != svc.Stopped {
		return fmt.Errorf(
			"service %q is still running; stop it with \"sc.exe stop %s\" before uninstalling",
			windowsServiceName,
			windowsServiceName,
		)
	}
	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete Windows service: %w", err)
	}
	if err := eventlog.Remove(windowsServiceName); err != nil {
		return fmt.Errorf("remove Windows Event Log source: %w", err)
	}
	return nil
}
