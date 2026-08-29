//go:build linux

/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

const (
	transactionTimeout  = 500 * time.Millisecond
	activeProbeTimeout  = 50 * time.Millisecond
	acceptRetryInitial  = 5 * time.Millisecond
	acceptRetryMaximum  = time.Second
	maxHandlers         = 512
	serverDirectoryMode = 0o711
	serverSocketMode    = 0o666
	serverLockMode      = 0o600
)

type socketIdentity struct {
	device uint64
	inode  uint64
}

type Broker struct {
	listener     *net.UnixListener
	socketPath   string
	socket       socketIdentity
	lockFile     *os.File
	handlerSlots chan struct{}
	handlerWG    sync.WaitGroup
	handlerMu    sync.Mutex
	closing      atomic.Bool
	dropped      atomic.Uint64
	closeOnce    sync.Once
	closeErr     error
}

func ListenDefault() (*Broker, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("the host PID broker must run as root")
	}
	return listen(ServerSocketPath, 0)
}

func listen(socketPath string, ownerUID int) (*Broker, error) {
	return listenWithHandlerLimit(socketPath, ownerUID, maxHandlers)
}

func listenWithHandlerLimit(socketPath string, ownerUID int,
	handlerLimit int) (*Broker, error) {
	if handlerLimit <= 0 {
		return nil, errors.New("host PID broker handler limit must be positive")
	}
	directory := filepath.Dir(socketPath)
	if err := prepareDirectory(directory, ownerUID); err != nil {
		return nil, err
	}

	lockFile, err := acquireLock(directory, ownerUID)
	if err != nil {
		return nil, err
	}
	releaseLock := true
	defer func() {
		if releaseLock {
			_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
			_ = lockFile.Close()
		}
	}()

	if err := removeStaleSocket(socketPath, ownerUID); err != nil {
		return nil, err
	}
	address := &net.UnixAddr{Name: socketPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return nil, fmt.Errorf("listen on host PID broker socket: %w", err)
	}
	listener.SetUnlinkOnClose(false)
	cleanupListener := true
	defer func() {
		if cleanupListener {
			_ = listener.Close()
			_ = os.Remove(socketPath)
		}
	}()

	if err := os.Chmod(socketPath, serverSocketMode); err != nil {
		return nil, fmt.Errorf("set host PID broker socket mode: %w", err)
	}
	identity, err := readSocketIdentity(socketPath, ownerUID)
	if err != nil {
		return nil, err
	}

	cleanupListener = false
	releaseLock = false
	return &Broker{
		listener:     listener,
		socketPath:   socketPath,
		socket:       identity,
		lockFile:     lockFile,
		handlerSlots: make(chan struct{}, handlerLimit),
	}, nil
}

func prepareDirectory(directory string, ownerUID int) error {
	if err := os.MkdirAll(directory, serverDirectoryMode); err != nil {
		return fmt.Errorf("create host PID broker directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect host PID broker directory: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("host PID broker directory is not a real directory")
	}
	if int(stat.Uid) != ownerUID {
		return fmt.Errorf("host PID broker directory owner is %d, want %d",
			stat.Uid, ownerUID)
	}
	if err := os.Chmod(directory, serverDirectoryMode); err != nil {
		return fmt.Errorf("set host PID broker directory mode: %w", err)
	}
	return nil
}

func acquireLock(directory string, ownerUID int) (*os.File, error) {
	lockPath := filepath.Join(directory, "broker.lock")
	fd, err := unix.Open(lockPath,
		unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		serverLockMode)
	if err != nil {
		return nil, fmt.Errorf("open host PID broker lock: %w", err)
	}
	lockFile := os.NewFile(uintptr(fd), lockPath)
	if lockFile == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open host PID broker lock file")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("lock host PID broker directory: %w", err)
	}

	info, err := lockFile.Stat()
	if err != nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, fmt.Errorf("inspect host PID broker lock: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || int(stat.Uid) != ownerUID ||
		info.Mode().Perm()&0o077 != 0 {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, errors.New("host PID broker lock is not trusted")
	}
	return lockFile, nil
}

func removeStaleSocket(socketPath string, ownerUID int) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect host PID broker socket: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 {
		return errors.New("host PID broker socket path is not a socket")
	}
	if int(stat.Uid) != ownerUID {
		return fmt.Errorf("host PID broker socket owner is %d, want %d",
			stat.Uid, ownerUID)
	}

	connection, dialErr := net.DialTimeout("unix", socketPath,
		activeProbeTimeout)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("another host PID broker is already listening")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) &&
		!errors.Is(dialErr, os.ErrNotExist) {
		return fmt.Errorf("probe existing host PID broker socket: %w", dialErr)
	}
	if err := os.Remove(socketPath); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale host PID broker socket: %w", err)
	}
	return nil
}

func readSocketIdentity(socketPath string, ownerUID int) (socketIdentity, error) {
	info, err := os.Lstat(socketPath)
	if err != nil {
		return socketIdentity{}, fmt.Errorf(
			"inspect new host PID broker socket: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 || int(stat.Uid) != ownerUID {
		return socketIdentity{}, errors.New("new host PID broker socket is not trusted")
	}
	return socketIdentity{device: uint64(stat.Dev), inode: stat.Ino}, nil
}

func (broker *Broker) Serve() error {
	var backoff time.Duration
	for {
		connection, err := broker.listener.AcceptUnix()
		if err != nil {
			if broker.closing.Load() {
				return nil
			}
			if isTemporaryAcceptError(err) {
				backoff = nextAcceptBackoff(backoff)
				time.Sleep(backoff)
				continue
			}
			return fmt.Errorf("accept host PID broker connection: %w", err)
		}
		backoff = 0
		if !broker.startHandler(connection) {
			_ = connection.Close()
		}
	}
}

func isTemporaryAcceptError(err error) bool {
	return errors.Is(err, syscall.EMFILE) ||
		errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.ENOBUFS) ||
		errors.Is(err, syscall.ENOMEM)
}

func nextAcceptBackoff(current time.Duration) time.Duration {
	if current == 0 {
		return acceptRetryInitial
	}
	next := current * 2
	if next > acceptRetryMaximum {
		return acceptRetryMaximum
	}
	return next
}

func (broker *Broker) startHandler(connection *net.UnixConn) bool {
	broker.handlerMu.Lock()
	defer broker.handlerMu.Unlock()
	if broker.closing.Load() {
		return false
	}
	select {
	case broker.handlerSlots <- struct{}{}:
		broker.handlerWG.Add(1)
		go broker.handle(connection)
		return true
	default:
		return false
	}
}

func (broker *Broker) handle(connection *net.UnixConn) {
	defer func() {
		_ = connection.Close()
		<-broker.handlerSlots
		broker.handlerWG.Done()
	}()
	_ = connection.SetDeadline(time.Now().Add(transactionTimeout))

	request := make([]byte, requestSize)
	if _, err := io.ReadFull(connection, request); err != nil {
		broker.logDroppedTransaction("request read", err)
		return
	}
	if !validRequest(request) {
		response := makeResponse(statusInvalidRequest, 0)
		writeResponse(connection, response)
		return
	}

	pid, err := peerPID(connection)
	if err != nil {
		broker.logDroppedTransaction("peer credentials", err)
		return
	}
	if pid <= 0 {
		return
	}
	response := makeResponse(statusOK, uint32(pid))
	writeResponse(connection, response)
}

func (broker *Broker) logDroppedTransaction(operation string, err error) {
	count := broker.dropped.Add(1)
	if count&(count-1) != 0 {
		return
	}
	klog.V(4).Infof(
		"Dropped host PID broker transaction during %s (count=%d): %v",
		operation, count, err)
}

func writeResponse(connection *net.UnixConn,
	response [responseSize]byte) {
	written := 0
	for written < len(response) {
		count, err := connection.Write(response[written:])
		if err != nil || count == 0 {
			return
		}
		written += count
	}
}

func peerPID(connection *net.UnixConn) (int32, error) {
	rawConnection, err := connection.SyscallConn()
	if err != nil {
		return 0, err
	}
	var credentials *unix.Ucred
	var credentialErr error
	if err := rawConnection.Control(func(fd uintptr) {
		credentials, credentialErr = unix.GetsockoptUcred(
			int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credentialErr != nil {
		return 0, credentialErr
	}
	if credentials == nil || credentials.Pid <= 0 {
		return 0, errors.New("host PID broker received invalid peer credentials")
	}
	return credentials.Pid, nil
}

func (broker *Broker) Close() error {
	broker.closeOnce.Do(func() {
		broker.handlerMu.Lock()
		broker.closing.Store(true)
		broker.handlerMu.Unlock()

		listenerErr := broker.listener.Close()
		broker.handlerWG.Wait()
		removeErr := broker.removeOwnedSocket()
		unlockErr := unix.Flock(int(broker.lockFile.Fd()), unix.LOCK_UN)
		lockCloseErr := broker.lockFile.Close()
		broker.closeErr = errors.Join(listenerErr, removeErr, unlockErr,
			lockCloseErr)
	})
	return broker.closeErr
}

func (broker *Broker) removeOwnedSocket() error {
	var stat unix.Stat_t
	if err := unix.Lstat(broker.socketPath, &stat); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect host PID broker socket during cleanup: %w",
			err)
	}
	if uint64(stat.Dev) != broker.socket.device ||
		stat.Ino != broker.socket.inode ||
		stat.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return nil
	}
	if err := unix.Unlink(broker.socketPath); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove host PID broker socket: %w", err)
	}
	return nil
}
