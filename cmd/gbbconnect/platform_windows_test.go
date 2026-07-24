//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

type fakeServiceEventLog struct {
	mu       sync.Mutex
	messages []string
}

func (log *fakeServiceEventLog) Info(_ uint32, message string) error {
	log.append(message)
	return nil
}

func (log *fakeServiceEventLog) Warning(_ uint32, message string) error {
	log.append(message)
	return nil
}

func (log *fakeServiceEventLog) Error(_ uint32, message string) error {
	log.append(message)
	return nil
}

func (*fakeServiceEventLog) Close() error {
	return nil
}

func (log *fakeServiceEventLog) append(message string) {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.messages = append(log.messages, message)
}

func (log *fakeServiceEventLog) contains(fragment string) bool {
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, message := range log.messages {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func TestWindowsServiceStopCancelsDaemon(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "gbbconnect.yaml")
	if err := os.WriteFile(configPath, []byte(runTestConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	eventLog := &fakeServiceEventLog{}
	service := &windowsService{
		buildVersion: "test",
		args: []string{
			"--config", configPath,
			"--state-dir", filepath.Join(directory, "state"),
			"run",
		},
		eventLog: eventLog,
	}
	requests := make(chan svc.ChangeRequest, 1)
	statuses := make(chan svc.Status, 8)
	result := make(chan struct {
		serviceSpecific bool
		exitCode        uint32
	}, 1)

	go func() {
		serviceSpecific, exitCode := service.Execute(nil, requests, statuses)
		result <- struct {
			serviceSpecific bool
			exitCode        uint32
		}{serviceSpecific: serviceSpecific, exitCode: exitCode}
	}()

	waitForServiceState(t, statuses, svc.Running)
	requests <- svc.ChangeRequest{Cmd: svc.Stop}
	waitForServiceState(t, statuses, svc.StopPending)

	select {
	case got := <-result:
		if got.serviceSpecific || got.exitCode != 0 {
			t.Fatalf("Execute() result = %#v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Windows service did not stop")
	}
	if !eventLog.contains("service stopped") {
		t.Fatal("service stop was not written to the Event Log")
	}
}

func waitForServiceState(
	t *testing.T,
	statuses <-chan svc.Status,
	want svc.State,
) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case status := <-statuses:
			if status.State == want {
				return
			}
		case <-timeout.C:
			t.Fatalf("did not receive Windows service state %d", want)
		}
	}
}
