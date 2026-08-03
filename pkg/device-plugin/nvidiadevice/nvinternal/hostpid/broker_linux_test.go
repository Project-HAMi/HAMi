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
	"sync"
	"syscall"
	"testing"
	"time"
)

const subprocessHelperEnvironment = "HAMI_HOSTPID_BROKER_HELPER"

func startTestBroker(t *testing.T) (*Broker, string) {
	t.Helper()
	directory := t.TempDir()
	socketPath := filepath.Join(directory, "broker.sock")
	broker, err := listen(socketPath, os.Geteuid())
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

func TestBrokerRejectsInvalidRequest(t *testing.T) {
	_, socketPath := startTestBroker(t)
	connection, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write(
		[]byte{'B', 'A', 'D', '!', 0, 1, 0, 1}); err != nil {
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
		"true":  true,
		"false": true,
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
	_, socketPath := startTestBroker(t)
	info, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		t.Fatalf("invalid socket stat: %#v", info.Sys())
	}
}
