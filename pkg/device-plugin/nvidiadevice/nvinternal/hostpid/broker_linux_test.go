//go:build linux

/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const subprocessHelperEnvironment = "HAMI_HOSTPID_BROKER_HELPER"

const admissionExitHelperEnvironment = "HAMI_HOSTPID_ADMISSION_EXIT_HELPER"

const externalCClientEnvironment = "HAMI_HOSTPID_C_CLIENT"

const externalCStormEnvironment = "HAMI_HOSTPID_C_STORM"

const externalCAdmissionEnvironment = "HAMI_HOSTPID_C_ADMISSION"

func startTestBroker(t *testing.T) (*Broker, string) {
	return startTestBrokerWithAdmission(t, 0)
}

func startTestBrokerWithAdmission(t *testing.T,
	permits int) (*Broker, string) {
	return startTestBrokerWithLimits(t, maxHandlers, permits)
}

func startTestBrokerWithLimits(t *testing.T, handlerLimit int,
	permits int) (*Broker, string) {
	return startTestBrokerWithMaxHold(t, handlerLimit, permits,
		DefaultDriverAdmissionMaxHold)
}

func startTestBrokerWithMaxHold(t *testing.T, handlerLimit int,
	permits int, maxHold time.Duration) (*Broker, string) {
	t.Helper()
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	broker, err := listenWithLimits(socketPath, os.Geteuid(), handlerLimit,
		permits, maxHold)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- broker.Serve()
	}()
	t.Cleanup(func() {
		if err := broker.Close(); err != nil {
			t.Errorf("close broker: %v", err)
		}
		if err := <-serveResult; err != nil {
			t.Errorf("serve broker: %v", err)
		}
	})
	return broker, socketPath
}

func acquireDriverAdmission(socketPath string,
	timeout time.Duration) (net.Conn, uint16, uint32, error) {
	return acquireDriverAdmissionCommand(socketPath, timeout,
		commandAcquireDriverInit)
}

func acquireDriverAdmissionCommand(socketPath string, timeout time.Duration,
	command uint16) (net.Conn, uint16, uint32, error) {
	connection, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return nil, 0, 0, err
	}
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = connection.Close()
		return nil, 0, 0, err
	}
	request := []byte{'H', 'P', 'I', 'D', 0, 1,
		byte(command >> 8), byte(command)}
	if _, err := connection.Write(request); err != nil {
		_ = connection.Close()
		return nil, 0, 0, err
	}
	response := make([]byte, responseSize)
	if _, err := io.ReadFull(connection, response); err != nil {
		_ = connection.Close()
		return nil, 0, 0, err
	}
	if string(response[:4]) != "HPID" ||
		binary.BigEndian.Uint16(response[4:6]) != protocolVersion {
		_ = connection.Close()
		return nil, 0, 0, errors.New("invalid broker response")
	}
	return connection, binary.BigEndian.Uint16(response[6:8]),
		binary.BigEndian.Uint32(response[8:12]), nil
}

func openAdmissionRequest(t *testing.T, socketPath string,
	command uint16) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	request := []byte{'H', 'P', 'I', 'D', 0, 1,
		byte(command >> 8), byte(command)}
	if _, err := connection.Write(request); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	return connection
}

func queryBroker(socketPath string) (uint16, uint32, error) {
	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return 0, 0, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return 0, 0, err
	}
	request := []byte{'H', 'P', 'I', 'D', 0, 1, 0, 1}
	if _, err := connection.Write(request); err != nil {
		return 0, 0, err
	}
	response := make([]byte, responseSize)
	if _, err := io.ReadFull(connection, response); err != nil {
		return 0, 0, err
	}
	if string(response[:4]) != "HPID" ||
		binary.BigEndian.Uint16(response[4:6]) != protocolVersion {
		return 0, 0, errors.New("invalid broker response")
	}
	return binary.BigEndian.Uint16(response[6:8]),
		binary.BigEndian.Uint32(response[8:12]), nil
}

func TestBrokerReturnsPeerPID(t *testing.T) {
	_, socketPath := startTestBroker(t)
	status, pid, err := queryBroker(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if status != statusOK || pid != uint32(os.Getpid()) {
		t.Fatalf("got status %d PID %d, want status 0 PID %d",
			status, pid, os.Getpid())
	}
}

func TestBrokerReturnsSubprocessPID(t *testing.T) {
	_, socketPath := startTestBroker(t)
	command := exec.Command(os.Args[0],
		"-test.run=^TestBrokerSubprocessHelper$")
	command.Env = append(os.Environ(),
		subprocessHelperEnvironment+"="+socketPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, output)
	}
}

func TestBrokerExternalCClient(t *testing.T) {
	clientPath := os.Getenv(externalCClientEnvironment)
	if clientPath == "" {
		t.Skip("HAMI_HOSTPID_C_CLIENT is not set")
	}
	_, socketPath := startTestBroker(t)
	command := exec.Command(clientPath, socketPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("external C client failed: %v\n%s", err, output)
	}
}

func TestBrokerExternalCClientStorm(t *testing.T) {
	clientPath := os.Getenv(externalCStormEnvironment)
	if clientPath == "" {
		t.Skip("HAMI_HOSTPID_C_STORM is not set")
	}
	_, socketPath := startTestBroker(t)
	command := exec.Command(clientPath, socketPath, "300")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("external C client storm failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output),
		"clients=300 successful=300 failed=0") {
		t.Fatalf("unexpected C client storm output: %s", output)
	}
}

func TestBrokerExternalCAdmissionContract(t *testing.T) {
	clientPath := os.Getenv(externalCAdmissionEnvironment)
	if clientPath == "" {
		t.Skip("HAMI_HOSTPID_C_ADMISSION is not set")
	}
	t.Run("bounded storm", func(t *testing.T) {
		_, socketPath := startTestBrokerWithAdmission(t, 5)
		command := exec.Command(clientPath, "storm", socketPath,
			"64", "4", "5")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("C admission storm failed: %v\n%s", err, output)
		}
	})
	t.Run("process death", func(t *testing.T) {
		_, socketPath := startTestBrokerWithAdmission(t, 2)
		command := exec.Command(clientPath, "death", socketPath)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("C admission process-death test failed: %v\n%s",
				err, output)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		_, socketPath := startTestBroker(t)
		command := exec.Command(clientPath, "disabled", socketPath)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("C disabled admission test failed: %v\n%s", err,
				output)
		}
	})
}

func TestBrokerSubprocessHelper(t *testing.T) {
	socketPath := os.Getenv(subprocessHelperEnvironment)
	if socketPath == "" {
		return
	}
	status, pid, err := queryBroker(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if status != statusOK || pid != uint32(os.Getpid()) {
		t.Fatalf("got status %d PID %d, want status 0 PID %d",
			status, pid, os.Getpid())
	}
}

func TestBrokerDriverAdmissionReleasesOnProcessExit(t *testing.T) {
	_, socketPath := startTestBrokerWithAdmission(t, 2)
	command := exec.Command(os.Args[0],
		"-test.run=^TestBrokerAdmissionExitHelper$")
	command.Env = append(os.Environ(),
		admissionExitHelperEnvironment+"="+socketPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, output)
	}
	connection, status, _, err := acquireDriverAdmission(socketPath,
		time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("admission after process exit: status=%d err=%v",
			status, err)
	}
	_ = connection.Close()
}

func TestBrokerAdmissionExitHelper(t *testing.T) {
	socketPath := os.Getenv(admissionExitHelperEnvironment)
	if socketPath == "" {
		return
	}
	connection, status, _, err := acquireDriverAdmission(socketPath,
		time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("admission: status=%d err=%v", status, err)
	}
	_ = connection
}

func TestBrokerRejectsInvalidRequest(t *testing.T) {
	_, socketPath := startTestBroker(t)
	tests := map[string][]byte{
		"magic":   {'B', 'A', 'D', '!', 0, 1, 0, 1},
		"version": {'H', 'P', 'I', 'D', 0, 2, 0, 1},
		"command": {'H', 'P', 'I', 'D', 0, 1, 0, 5},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			connection, err := net.Dial("unix", socketPath)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if _, err := connection.Write(request); err != nil {
				t.Fatal(err)
			}
			response := make([]byte, responseSize)
			if _, err := io.ReadFull(connection, response); err != nil {
				t.Fatal(err)
			}
			if status := binary.BigEndian.Uint16(response[6:8]); status != statusInvalidRequest {
				t.Fatalf("got status %d", status)
			}
			if pid := binary.BigEndian.Uint32(response[8:12]); pid != 0 {
				t.Fatalf("got PID %d", pid)
			}
		})
	}
}

func TestBrokerReportsDisabledDriverAdmission(t *testing.T) {
	_, socketPath := startTestBroker(t)
	connection, status, payload, err := acquireDriverAdmission(socketPath,
		time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if status != statusUnavailable || payload != 0 {
		t.Fatalf("status=%d payload=%d", status, payload)
	}
}

func TestBrokerBoundsAndReleasesDriverAdmission(t *testing.T) {
	broker, socketPath := startTestBrokerWithAdmission(t, 3)
	first, status, _, err := acquireDriverAdmission(socketPath, time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("first admission: status=%d err=%v", status, err)
	}
	defer first.Close()
	second, status, _, err := acquireDriverAdmission(socketPath, time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("second admission: status=%d err=%v", status, err)
	}
	defer second.Close()
	if got := broker.admission.activeCount(); got != 2 {
		t.Fatalf("active admissions=%d, want 2", got)
	}

	type result struct {
		connection net.Conn
		status     uint16
		err        error
	}
	thirdResult := make(chan result, 1)
	go func() {
		connection, status, _, err := acquireDriverAdmission(socketPath,
			2*time.Second)
		thirdResult <- result{connection: connection, status: status, err: err}
	}()
	select {
	case result := <-thirdResult:
		if result.connection != nil {
			_ = result.connection.Close()
		}
		t.Fatalf("third admission did not wait: status=%d err=%v",
			result.status, result.err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-thirdResult:
		if result.err != nil || result.status != statusOK {
			t.Fatalf("third admission: status=%d err=%v",
				result.status, result.err)
		}
		_ = result.connection.Close()
	case <-time.After(time.Second):
		t.Fatal("third admission was not granted after release")
	}
}

func TestBrokerReservesReleaseCapacity(t *testing.T) {
	broker, socketPath := startTestBrokerWithAdmission(t, 2)
	retain, status, _, err := acquireDriverAdmissionCommand(socketPath,
		time.Second, commandAcquireDriverRetain)
	if err != nil || status != statusOK {
		t.Fatalf("first retain admission: status=%d err=%v", status, err)
	}
	defer retain.Close()

	type result struct {
		connection net.Conn
		status     uint16
		err        error
	}
	secondRetain := make(chan result, 1)
	go func() {
		connection, status, _, err := acquireDriverAdmissionCommand(
			socketPath, 2*time.Second, commandAcquireDriverRetain)
		secondRetain <- result{connection: connection, status: status, err: err}
	}()
	select {
	case got := <-secondRetain:
		if got.connection != nil {
			_ = got.connection.Close()
		}
		t.Fatalf("second retain did not wait: status=%d err=%v",
			got.status, got.err)
	case <-time.After(50 * time.Millisecond):
	}

	release, status, _, err := acquireDriverAdmissionCommand(socketPath,
		time.Second, commandAcquireDriverRelease)
	if err != nil || status != statusOK {
		t.Fatalf("release admission: status=%d err=%v", status, err)
	}
	if got := broker.admission.activeCount(); got != 2 {
		t.Fatalf("active admissions=%d, want 2", got)
	}
	if err := release.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-secondRetain:
		if got.connection != nil {
			_ = got.connection.Close()
		}
		t.Fatalf("release slot admitted another retain: status=%d err=%v",
			got.status, got.err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := retain.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-secondRetain:
		if got.err != nil || got.status != statusOK {
			t.Fatalf("second retain: status=%d err=%v", got.status, got.err)
		}
		_ = got.connection.Close()
	case <-time.After(time.Second):
		t.Fatal("second retain was not granted after forward release")
	}
}

func TestBrokerReservesReleaseHandlerSlot(t *testing.T) {
	const handlerLimit = 8
	broker, socketPath := startTestBrokerWithLimits(t, handlerLimit, 2)
	nonReleaseBudget := admissionHandlerBudget(handlerLimit) - 1
	connections := make([]net.Conn, 0, nonReleaseBudget)
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	for range nonReleaseBudget {
		connections = append(connections,
			openAdmissionRequest(t, socketPath, commandAcquireDriverRetain))
	}
	deadline := time.Now().Add(time.Second)
	for broker.admissionHandlers.Load() != int64(nonReleaseBudget) {
		if time.Now().After(deadline) {
			t.Fatalf("admission handlers=%d, want %d",
				broker.admissionHandlers.Load(), nonReleaseBudget)
		}
		time.Sleep(time.Millisecond)
	}

	overflow := openAdmissionRequest(t, socketPath,
		commandAcquireDriverRetain)
	defer func() { _ = overflow.Close() }()
	if err := overflow.SetReadDeadline(
		time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, responseSize)
	_, readErr := io.ReadFull(overflow, response)
	if readErr != nil {
		t.Errorf("retain above the reserved budget was not rejected: %v",
			readErr)
	} else if status := binary.BigEndian.Uint16(response[6:8]); status !=
		statusUnavailable {
		t.Errorf("retain above the reserved budget: status=%d", status)
	}

	release, status, _, err := acquireDriverAdmissionCommand(socketPath,
		time.Second, commandAcquireDriverRelease)
	if err != nil || status != statusOK {
		t.Fatalf("release admission under handler pressure: status=%d err=%v",
			status, err)
	}
	_ = release.Close()
}

func TestBrokerDriverAdmissionConcurrentBound(t *testing.T) {
	const permits = 4
	const forwardPermits = permits - 1
	const clients = 64
	_, socketPath := startTestBrokerWithAdmission(t, permits)
	var active atomic.Int32
	var maximum atomic.Int32
	errorsChannel := make(chan error, clients)
	var waitGroup sync.WaitGroup

	for range clients {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			connection, status, _, err := acquireDriverAdmission(socketPath,
				3*time.Second)
			if err != nil || status != statusOK {
				errorsChannel <- fmt.Errorf("status=%d err=%v", status, err)
				return
			}
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			if current > forwardPermits {
				errorsChannel <- fmt.Errorf("active admissions=%d", current)
			}
			time.Sleep(2 * time.Millisecond)
			active.Add(-1)
			_ = connection.Close()
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
	if maximum.Load() != forwardPermits {
		t.Fatalf("maximum active admissions=%d, want %d",
			maximum.Load(), forwardPermits)
	}
}

func TestBrokerAdmissionIgnoresClientBytes(t *testing.T) {
	broker, socketPath := startTestBrokerWithAdmission(t, 2)
	connection, status, _, err := acquireDriverAdmission(socketPath,
		time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("admission: status=%d err=%v", status, err)
	}
	defer func() { _ = connection.Close() }()
	if got := broker.admission.activeCount(); got != 1 {
		t.Fatalf("active admissions=%d, want 1", got)
	}

	if _, err := connection.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := broker.admission.activeCount(); got != 1 {
		t.Fatalf("client data released the lease: active=%d", got)
	}

	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for broker.admission.activeCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("lease was not released after the client closed")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBrokerRejectsAdmissionAboveHandlerBudget(t *testing.T) {
	const handlerLimit = 8
	broker, socketPath := startTestBrokerWithLimits(t, handlerLimit, 2)
	nonReleaseBudget := admissionHandlerBudget(handlerLimit) - 1
	connections := make([]net.Conn, 0, nonReleaseBudget)
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	for range nonReleaseBudget {
		connections = append(connections,
			openAdmissionRequest(t, socketPath, commandAcquireDriverInit))
	}
	deadline := time.Now().Add(time.Second)
	for broker.admissionHandlers.Load() != int64(nonReleaseBudget) {
		if time.Now().After(deadline) {
			t.Fatalf("admission handlers=%d, want %d",
				broker.admissionHandlers.Load(), nonReleaseBudget)
		}
		time.Sleep(time.Millisecond)
	}

	rejected := openAdmissionRequest(t, socketPath,
		commandAcquireDriverInit)
	defer func() { _ = rejected.Close() }()
	if err := rejected.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, responseSize)
	if _, err := io.ReadFull(rejected, response); err != nil {
		t.Fatal(err)
	}
	if status := binary.BigEndian.Uint16(response[6:8]); status !=
		statusUnavailable {
		t.Fatalf("admission above the handler budget: status=%d", status)
	}

	status, pid, err := queryBroker(socketPath)
	if err != nil || status != statusOK || pid != uint32(os.Getpid()) {
		t.Fatalf("PID query under admission pressure: status=%d pid=%d err=%v",
			status, pid, err)
	}
}

func TestDriverAdmissionCancelsQueuedWaiter(t *testing.T) {
	controller := newDriverAdmissionController(2)
	shutdown := make(chan struct{})
	idle := make(chan struct{})
	if !controller.acquire(driverAdmissionRetain, idle, shutdown) {
		t.Fatal("initial admission was not granted")
	}

	connectionDone := make(chan struct{})
	result := make(chan bool, 1)
	go func() {
		result <- controller.acquire(driverAdmissionInit, connectionDone,
			shutdown)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		controller.mutex.Lock()
		queued := len(controller.queues[driverAdmissionInit])
		controller.mutex.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("admission request did not queue")
		}
		time.Sleep(time.Millisecond)
	}

	close(connectionDone)
	select {
	case granted := <-result:
		if granted {
			t.Fatal("canceled admission was granted")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled admission did not return")
	}
	if got := controller.activeCount(); got != 1 {
		t.Fatalf("active admissions=%d, want 1", got)
	}

	controller.release(driverAdmissionRetain)
	if !controller.acquire(driverAdmissionInit, idle, shutdown) {
		t.Fatal("admission after cancellation was not granted")
	}
}

func TestDriverAdmissionPrioritizesLifecycleCompletion(t *testing.T) {
	controller := newDriverAdmissionController(2)
	connectionDone := make(chan struct{})
	shutdown := make(chan struct{})
	if !controller.acquire(driverAdmissionRetain, connectionDone, shutdown) {
		t.Fatal("initial admission was not granted")
	}

	order := make(chan driverAdmissionPriority, 2)
	releaseGates := map[driverAdmissionPriority]chan struct{}{
		driverAdmissionInit:   make(chan struct{}),
		driverAdmissionRetain: make(chan struct{}),
	}
	for _, priority := range []driverAdmissionPriority{
		driverAdmissionInit,
		driverAdmissionRetain,
	} {
		priority := priority
		go func() {
			if !controller.acquire(priority, connectionDone, shutdown) {
				return
			}
			order <- priority
			<-releaseGates[priority]
			controller.release(priority)
		}()
	}

	deadline := time.Now().Add(time.Second)
	for {
		controller.mutex.Lock()
		queued := len(controller.queues[driverAdmissionInit]) +
			len(controller.queues[driverAdmissionRetain])
		controller.mutex.Unlock()
		if queued == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("admission requests did not queue")
		}
		time.Sleep(time.Millisecond)
	}

	if !controller.acquire(driverAdmissionRelease, connectionDone, shutdown) {
		t.Fatal("reserved release admission was not granted")
	}
	if got := controller.activeCount(); got != 2 {
		t.Fatalf("active admissions=%d, want 2", got)
	}
	controller.release(driverAdmissionRelease)
	controller.release(driverAdmissionRetain)
	for _, expected := range []driverAdmissionPriority{
		driverAdmissionRetain,
		driverAdmissionInit,
	} {
		select {
		case got := <-order:
			if got != expected {
				t.Fatalf("grant priority=%d, want %d", got, expected)
			}
			close(releaseGates[got])
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for priority %d", expected)
		}
	}
}

func TestDriverAdmissionBoundsRetainPriorityBurst(t *testing.T) {
	controller := newDriverAdmissionController(2)
	controller.mutex.Lock()
	controller.retainPriorityGrants = maximumRetainPriorityBurst
	controller.queues[driverAdmissionRetain] = append(
		controller.queues[driverAdmissionRetain],
		&driverAdmissionWaiter{granted: make(chan struct{})})
	controller.queues[driverAdmissionInit] = append(
		controller.queues[driverAdmissionInit],
		&driverAdmissionWaiter{granted: make(chan struct{})})
	priority, ok := controller.nextForwardPriorityLocked()
	controller.mutex.Unlock()
	if !ok || priority != driverAdmissionInit {
		t.Fatalf("priority=%d valid=%v, want initialization", priority, ok)
	}
}

func TestBrokerDriverAdmissionReleasesBeforeMaxHold(t *testing.T) {
	_, socketPath := startTestBrokerWithAdmission(t, 2)
	first, status, _, err := acquireDriverAdmission(socketPath, time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("first admission: status=%d err=%v", status, err)
	}
	defer first.Close()

	result := make(chan error, 1)
	go func() {
		connection, status, _, err := acquireDriverAdmission(socketPath,
			500*time.Millisecond)
		if connection != nil {
			_ = connection.Close()
		}
		if err == nil && status != statusOK {
			err = fmt.Errorf("status=%d", status)
		}
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("queued admission completed while the lease was active: %v",
			err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("queued admission after release: %v", err)
	}
}

func waitForActiveAdmissions(t *testing.T, broker *Broker, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for broker.admission.activeCount() != want {
		if time.Now().After(deadline) {
			t.Fatalf("active admissions=%d, want %d",
				broker.admission.activeCount(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBrokerDriverAdmissionExpiresAfterMaxHold(t *testing.T) {
	const maxHold = 300 * time.Millisecond
	broker, socketPath := startTestBrokerWithMaxHold(t, maxHandlers, 2,
		maxHold)
	stale, status, _, err := acquireDriverAdmission(socketPath, time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("stale admission: status=%d err=%v", status, err)
	}
	defer stale.Close()
	granted := time.Now()

	type result struct {
		connection net.Conn
		status     uint16
		err        error
	}
	waiting := make(chan result, 1)
	go func() {
		connection, status, _, err := acquireDriverAdmission(socketPath,
			2*time.Second)
		waiting <- result{connection: connection, status: status, err: err}
	}()
	var next net.Conn
	select {
	case result := <-waiting:
		if result.err != nil || result.status != statusOK {
			t.Fatalf("waiting admission: status=%d err=%v",
				result.status, result.err)
		}
		next = result.connection
	case <-time.After(2 * time.Second):
		t.Fatal("waiting admission was not granted after the lease expired")
	}
	defer next.Close()
	if elapsed := time.Since(granted); elapsed < maxHold {
		t.Fatalf("lease expired after %s, want at least %s", elapsed, maxHold)
	}
	if got := broker.admission.activeCount(); got != 1 {
		t.Fatalf("active admissions=%d, want 1", got)
	}

	// A late close from the expired client must not free the new lease.
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	queued := openAdmissionRequest(t, socketPath, commandAcquireDriverRetain)
	defer queued.Close()
	if err := queued.SetReadDeadline(
		time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, responseSize)
	if _, err := io.ReadFull(queued, response); err == nil {
		t.Fatal("queued admission was granted after a late release")
	}
	if got := broker.admission.activeCount(); got != 1 {
		t.Fatalf("active admissions after late release=%d, want 1", got)
	}
}

func TestBrokerDriverAdmissionLateReleaseIsNoop(t *testing.T) {
	broker, socketPath := startTestBrokerWithMaxHold(t, maxHandlers, 3,
		300*time.Millisecond)
	stale, status, _, err := acquireDriverAdmission(socketPath, time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("stale admission: status=%d err=%v", status, err)
	}
	defer stale.Close()
	waitForActiveAdmissions(t, broker, 0)

	first, status, _, err := acquireDriverAdmission(socketPath, time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("first admission: status=%d err=%v", status, err)
	}
	defer first.Close()
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	if got := broker.admission.activeCount(); got != 1 {
		t.Fatalf("active admissions after late release=%d, want 1", got)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	waitForActiveAdmissions(t, broker, 0)
	broker.admission.mutex.Lock()
	activeForward := broker.admission.activeForward
	broker.admission.mutex.Unlock()
	if activeForward != 0 {
		t.Fatalf("active forward admissions=%d, want 0", activeForward)
	}
}

func TestBrokerCloseBoundsDriverAdmission(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	broker, err := listenWithLimits(socketPath, os.Geteuid(), maxHandlers, 2,
		DefaultDriverAdmissionMaxHold)
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- broker.Serve() }()

	connection, status, _, err := acquireDriverAdmission(socketPath,
		time.Second)
	if err != nil || status != statusOK {
		t.Fatalf("admission: status=%d err=%v", status, err)
	}
	begin := time.Now()
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("close took %s", elapsed)
	}
	if err := <-serveResult; err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
}

func TestBrokerRejectsInvalidAdmissionLimit(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	for _, permits := range []int{-1, 1, MaximumDriverAdmissionPermits + 1} {
		broker, err := listenWithLimits(socketPath, os.Geteuid(), maxHandlers,
			permits, DefaultDriverAdmissionMaxHold)
		if err == nil {
			_ = broker.Close()
			t.Fatalf("accepted admission permits %d", permits)
		}
	}
	for _, maxHold := range []time.Duration{-time.Second, 0,
		MaximumDriverAdmissionMaxHold + time.Nanosecond} {
		broker, err := listenWithLimits(socketPath, os.Geteuid(), maxHandlers,
			2, maxHold)
		if err == nil {
			_ = broker.Close()
			t.Fatalf("accepted admission max hold %s", maxHold)
		}
	}
}

func TestBrokerRecoversFromEarlyClose(t *testing.T) {
	_, socketPath := startTestBroker(t)
	connection, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}

	status, pid, err := queryBroker(socketPath)
	if err != nil || status != statusOK || pid != uint32(os.Getpid()) {
		t.Fatalf("broker did not recover: status=%d pid=%d err=%v",
			status, pid, err)
	}
}

func TestTemporaryAcceptErrors(t *testing.T) {
	for _, acceptErr := range []error{
		syscall.EMFILE,
		syscall.ENFILE,
		syscall.ENOBUFS,
		syscall.ENOMEM,
		fmt.Errorf("wrapped: %w", syscall.EMFILE),
	} {
		if !isTemporaryAcceptError(acceptErr) {
			t.Errorf("expected temporary accept error: %v", acceptErr)
		}
	}
	if isTemporaryAcceptError(syscall.EINVAL) {
		t.Fatal("EINVAL must remain a permanent accept error")
	}
}

func TestAcceptBackoffIsBounded(t *testing.T) {
	backoff := time.Duration(0)
	for range 32 {
		backoff = nextAcceptBackoff(backoff)
		if backoff > acceptRetryMaximum {
			t.Fatalf("backoff %v exceeds maximum %v", backoff,
				acceptRetryMaximum)
		}
	}
	if backoff != acceptRetryMaximum {
		t.Fatalf("backoff=%v, want %v", backoff, acceptRetryMaximum)
	}
}

func TestBrokerTimesOutPartialRequest(t *testing.T) {
	_, socketPath := startTestBroker(t)
	connection, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte{'H', 'P', 'I', 'D'}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(transactionTimeout + 100*time.Millisecond)
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	if count, err := connection.Read(buffer); count != 0 || err == nil {
		t.Fatalf("partial request connection stayed open: n=%d err=%v",
			count, err)
	}
	_ = connection.Close()

	status, pid, err := queryBroker(socketPath)
	if err != nil || status != statusOK || pid != uint32(os.Getpid()) {
		t.Fatalf("broker did not recover: status=%d pid=%d err=%v",
			status, pid, err)
	}
}

func TestBrokerHandlesConcurrentClients(t *testing.T) {
	_, socketPath := startTestBroker(t)
	const clients = 300
	errorsChannel := make(chan error, clients)
	var waitGroup sync.WaitGroup

	for range clients {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			status, pid, err := queryBroker(socketPath)
			if err != nil {
				errorsChannel <- err
				return
			}
			if status != statusOK || pid != uint32(os.Getpid()) {
				errorsChannel <- fmt.Errorf("status=%d pid=%d", status, pid)
			}
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
}

func TestBrokerBoundsSlowClientsAndRecovers(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	const handlerLimit = 4
	broker, err := listenWithHandlerLimit(socketPath, os.Geteuid(),
		handlerLimit)
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- broker.Serve() }()
	t.Cleanup(func() {
		if err := broker.Close(); err != nil {
			t.Errorf("close broker: %v", err)
		}
		if err := <-serveResult; err != nil {
			t.Errorf("serve broker: %v", err)
		}
	})

	connections := make([]net.Conn, 0, handlerLimit)
	for range handlerLimit {
		connection, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
	}
	deadline := time.Now().Add(time.Second)
	for len(broker.handlerSlots) != handlerLimit && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(broker.handlerSlots); got != handlerLimit {
		t.Fatalf("active handlers=%d, want %d", got, handlerLimit)
	}

	overflow, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := overflow.SetDeadline(time.Now().Add(transactionTimeout)); err != nil {
		t.Fatal(err)
	}
	request := []byte{'H', 'P', 'I', 'D', 0, 1, 0, 1}
	_, _ = overflow.Write(request)
	response := make([]byte, responseSize)
	if _, err := io.ReadFull(overflow, response); err == nil {
		t.Fatal("overflow client received a response")
	}
	_ = overflow.Close()

	for _, connection := range connections {
		_ = connection.Close()
	}
	deadline = time.Now().Add(2 * transactionTimeout)
	for {
		status, pid, queryErr := queryBroker(socketPath)
		if queryErr == nil && status == statusOK && pid == uint32(os.Getpid()) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("broker did not recover: status=%d pid=%d err=%v",
				status, pid, queryErr)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBrokerCloseBoundsPartialClient(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	broker, err := listenWithHandlerLimit(socketPath, os.Geteuid(), 1)
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- broker.Serve() }()

	connection, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte{'H'}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(broker.handlerSlots) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	begin := time.Now()
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(begin); elapsed > 2*transactionTimeout {
		t.Fatalf("close took %s", elapsed)
	}
	if err := <-serveResult; err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
}

func TestBrokerRestartsAfterClose(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")

	for attempt := range 2 {
		broker, err := listen(socketPath, os.Geteuid())
		if err != nil {
			t.Fatalf("listen attempt %d: %v", attempt, err)
		}
		serveResult := make(chan error, 1)
		go func() { serveResult <- broker.Serve() }()
		status, pid, err := queryBroker(socketPath)
		if err != nil || status != statusOK || pid != uint32(os.Getpid()) {
			t.Fatalf("query attempt %d: status=%d pid=%d err=%v",
				attempt, status, pid, err)
		}
		if err := broker.Close(); err != nil {
			t.Fatalf("close attempt %d: %v", attempt, err)
		}
		if err := <-serveResult; err != nil {
			t.Fatalf("serve attempt %d: %v", attempt, err)
		}
	}
}

func TestBrokerCreatesTrustedModes(t *testing.T) {
	_, socketPath := startTestBroker(t)
	directoryInfo, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		t.Fatal(err)
	}
	socketInfo, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != serverDirectoryMode {
		t.Fatalf("directory mode is %#o", got)
	}
	if got := socketInfo.Mode().Perm(); got != serverSocketMode {
		t.Fatalf("socket mode is %#o", got)
	}
}

func TestBrokerRejectsPathCollision(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	if err := os.WriteFile(socketPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if broker, err := listen(socketPath, os.Geteuid()); err == nil {
		_ = broker.Close()
		t.Fatal("broker accepted a regular file collision")
	}
	contents, err := os.ReadFile(socketPath)
	if err != nil || string(contents) != "keep" {
		t.Fatalf("collision was changed: contents=%q err=%v", contents, err)
	}
}

func TestBrokerRemovesStaleSocket(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: socketPath,
		Net:  "unix",
	})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	broker, err := listen(socketPath, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBrokerDoesNotRemoveActiveSocket(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: socketPath,
		Net:  "unix",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	broker, err := listen(socketPath, os.Geteuid())
	if err == nil {
		_ = broker.Close()
		t.Fatal("broker replaced an active socket")
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatalf("active socket was removed: %v", err)
	}
}

func TestBrokerRejectsSymlinkDirectory(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	link := filepath.Join(parent, "link")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	broker, err := listen(filepath.Join(link, "broker.sock"), os.Geteuid())
	if err == nil {
		_ = broker.Close()
		t.Fatal("broker accepted a symlink directory")
	}
}

func TestBrokerRejectsSymlinkLock(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	lockPath := filepath.Join(directory, "broker.lock")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, lockPath); err != nil {
		t.Fatal(err)
	}
	broker, err := listen(filepath.Join(directory, "broker.sock"),
		os.Geteuid())
	if err == nil {
		_ = broker.Close()
		t.Fatal("broker accepted a symlink lock")
	}
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "keep" {
		t.Fatalf("lock target was changed: contents=%q err=%v", contents, err)
	}
}

func TestBrokerRejectsSecondListener(t *testing.T) {
	broker, socketPath := startTestBroker(t)
	second, err := listen(socketPath, os.Geteuid())
	if err == nil {
		_ = second.Close()
		t.Fatal("second broker acquired the socket")
	}
	status, pid, queryErr := queryBroker(socketPath)
	if queryErr != nil || status != statusOK || pid != uint32(os.Getpid()) {
		t.Fatalf("first broker stopped: status=%d pid=%d err=%v",
			status, pid, queryErr)
	}
	_ = broker
}

func TestBrokerLeavesReplacementDuringClose(t *testing.T) {
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	oldSocketPath := filepath.Join(directory, "old.sock")
	broker, err := listen(socketPath, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- broker.Serve() }()

	if err := os.Rename(socketPath, oldSocketPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socketPath, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-serveResult; err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(socketPath)
	if err != nil || string(contents) != "replacement" {
		t.Fatalf("replacement was changed: contents=%q err=%v", contents, err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(oldSocketPath); err != nil {
		t.Fatal(err)
	}
}

func TestEnabled(t *testing.T) {
	tests := map[string]bool{
		"":      false,
		"0":     false,
		"1":     true,
		"true":  false,
		"false": false,
		"01":    false,
		" 1":    false,
	}
	for value, expected := range tests {
		t.Run(strconv.Quote(value), func(t *testing.T) {
			if got := Enabled(value); got != expected {
				t.Fatalf("Enabled(%q)=%v, want %v", value, got, expected)
			}
		})
	}
}

func TestListenDefaultRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test requires a non-root process")
	}
	broker, err := ListenDefault()
	if broker != nil || err == nil {
		t.Fatalf("broker=%v err=%v", broker, err)
	}
}

func TestBrokerSocketIdentityUsesDeviceAndInode(t *testing.T) {
	broker, socketPath := startTestBroker(t)
	info, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		t.Fatalf("invalid socket stat: %#v", info.Sys())
	}
	if uint64(stat.Dev) != broker.socket.device ||
		stat.Ino != broker.socket.inode {
		t.Fatalf("recorded identity dev=%d ino=%d, want dev=%d ino=%d",
			broker.socket.device, broker.socket.inode, stat.Dev, stat.Ino)
	}
}
