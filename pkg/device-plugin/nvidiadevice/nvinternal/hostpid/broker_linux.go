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
	transactionTimeout         = 500 * time.Millisecond
	activeProbeTimeout         = 50 * time.Millisecond
	acceptRetryInitial         = 5 * time.Millisecond
	acceptRetryMaximum         = time.Second
	maxHandlers                = 512
	pidHandlerReserve          = 64
	serverDirectoryMode        = 0o711
	serverSocketMode           = 0o666
	serverLockMode             = 0o600
	maximumRetainPriorityBurst = 8
)

type socketIdentity struct {
	device uint64
	inode  uint64
}

type driverAdmissionPriority uint8

const (
	driverAdmissionRelease driverAdmissionPriority = iota
	driverAdmissionRetain
	driverAdmissionInit
	driverAdmissionPriorityCount
)

type driverAdmissionWaiterState uint8

const (
	driverAdmissionPending driverAdmissionWaiterState = iota
	driverAdmissionGranted
	driverAdmissionCanceled
)

type driverAdmissionWaiter struct {
	granted chan struct{}
	state   driverAdmissionWaiterState
}

type driverAdmissionController struct {
	mutex                sync.Mutex
	forwardPermits       int
	activeForward        int
	releaseActive        bool
	retainPriorityGrants int
	queues               [driverAdmissionPriorityCount][]*driverAdmissionWaiter
	closed               bool
}

func newDriverAdmissionController(permits int) *driverAdmissionController {
	if permits == 0 {
		return nil
	}
	return &driverAdmissionController{forwardPermits: permits - 1}
}

func admissionPriority(command uint16) (driverAdmissionPriority, bool) {
	switch command {
	case commandAcquireDriverInit:
		return driverAdmissionInit, true
	case commandAcquireDriverRetain:
		return driverAdmissionRetain, true
	case commandAcquireDriverRelease:
		return driverAdmissionRelease, true
	default:
		return 0, false
	}
}

func (controller *driverAdmissionController) acquire(
	priority driverAdmissionPriority, connectionDone <-chan struct{},
	shutdown <-chan struct{}) bool {
	waiter := &driverAdmissionWaiter{granted: make(chan struct{})}
	controller.mutex.Lock()
	if controller.closed {
		controller.mutex.Unlock()
		return false
	}
	controller.queues[priority] = append(controller.queues[priority], waiter)
	controller.grantLocked()
	controller.mutex.Unlock()

	select {
	case <-waiter.granted:
		return true
	case <-connectionDone:
		return controller.cancelOrObserveGrant(waiter)
	case <-shutdown:
		return controller.cancelOrObserveGrant(waiter)
	}
}

func (controller *driverAdmissionController) cancelOrObserveGrant(
	waiter *driverAdmissionWaiter) bool {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	if waiter.state == driverAdmissionPending {
		waiter.state = driverAdmissionCanceled
		controller.grantLocked()
		return false
	}
	return waiter.state == driverAdmissionGranted
}

func (controller *driverAdmissionController) release(
	priority driverAdmissionPriority) {
	controller.mutex.Lock()
	if priority == driverAdmissionRelease {
		controller.releaseActive = false
	} else if controller.activeForward > 0 {
		controller.activeForward--
	}
	controller.grantLocked()
	controller.mutex.Unlock()
}

func (controller *driverAdmissionController) close() {
	controller.mutex.Lock()
	controller.closed = true
	for priority := driverAdmissionRelease; priority < driverAdmissionPriorityCount; priority++ {
		for _, waiter := range controller.queues[priority] {
			if waiter.state == driverAdmissionPending {
				waiter.state = driverAdmissionCanceled
			}
		}
		controller.queues[priority] = nil
	}
	controller.mutex.Unlock()
}

func (controller *driverAdmissionController) grantLocked() {
	if controller.closed {
		return
	}
	if !controller.releaseActive {
		if waiter, ok := controller.dequeueLocked(driverAdmissionRelease); ok {
			waiter.state = driverAdmissionGranted
			controller.releaseActive = true
			close(waiter.granted)
		}
	}
	for controller.activeForward < controller.forwardPermits {
		priority, ok := controller.nextForwardPriorityLocked()
		if !ok {
			return
		}
		waiter, _ := controller.dequeueLocked(priority)
		waiter.state = driverAdmissionGranted
		controller.activeForward++
		if priority == driverAdmissionRetain {
			controller.retainPriorityGrants++
		} else if priority == driverAdmissionInit {
			controller.retainPriorityGrants = 0
		}
		close(waiter.granted)
	}
}

func (controller *driverAdmissionController) nextForwardPriorityLocked() (
	driverAdmissionPriority, bool) {
	retainPending := controller.hasPendingLocked(driverAdmissionRetain)
	initPending := controller.hasPendingLocked(driverAdmissionInit)
	if retainPending && (!initPending ||
		controller.retainPriorityGrants < maximumRetainPriorityBurst) {
		return driverAdmissionRetain, true
	}
	if initPending {
		return driverAdmissionInit, true
	}
	if retainPending {
		return driverAdmissionRetain, true
	}
	return 0, false
}

func (controller *driverAdmissionController) dequeueLocked(
	priority driverAdmissionPriority) (*driverAdmissionWaiter, bool) {
	if !controller.hasPendingLocked(priority) {
		return nil, false
	}
	queue := controller.queues[priority]
	waiter := queue[0]
	controller.queues[priority] = queue[1:]
	return waiter, true
}

func (controller *driverAdmissionController) hasPendingLocked(
	priority driverAdmissionPriority) bool {
	queue := controller.queues[priority]
	for len(queue) > 0 && queue[0].state != driverAdmissionPending {
		queue = queue[1:]
	}
	controller.queues[priority] = queue
	return len(queue) > 0
}

func (controller *driverAdmissionController) activeCount() int {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	active := controller.activeForward
	if controller.releaseActive {
		active++
	}
	return active
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
	admission    *driverAdmissionController
	// admissionHandlers tracks connections waiting for or holding a driver
	// admission lease. The budget leaves handler slots for PID queries, whose
	// clients cannot use the admission queue, and keeps one slot for release
	// requests.
	admissionHandlers      atomic.Int64
	admissionHandlerBudget int
	admissionMaxHold       time.Duration
	shutdown               chan struct{}
	closeOnce              sync.Once
	closeErr               error
}

func admissionHandlerBudget(handlerLimit int) int {
	reserve := handlerLimit / 4
	if reserve > pidHandlerReserve {
		reserve = pidHandlerReserve
	}
	return handlerLimit - reserve
}

func ListenDefault() (*Broker, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("the host PID broker must run as root")
	}
	permits, err := ParseDriverAdmissionPermits(
		os.Getenv(DriverAdmissionPermitsEnvironment))
	if err != nil {
		return nil, err
	}
	maxHold, err := ParseDriverAdmissionMaxHold(
		os.Getenv(DriverAdmissionMaxHoldEnvironment))
	if err != nil {
		return nil, err
	}
	return listenWithLimits(ServerSocketPath, 0, maxHandlers, permits, maxHold)
}

func listen(socketPath string, ownerUID int) (*Broker, error) {
	return listenWithLimits(socketPath, ownerUID, maxHandlers, 0,
		DefaultDriverAdmissionMaxHold)
}

func listenWithHandlerLimit(socketPath string, ownerUID int,
	handlerLimit int) (*Broker, error) {
	return listenWithLimits(socketPath, ownerUID, handlerLimit, 0,
		DefaultDriverAdmissionMaxHold)
}

func listenWithLimits(socketPath string, ownerUID int,
	handlerLimit int, admissionPermits int,
	admissionMaxHold time.Duration) (*Broker, error) {
	if handlerLimit <= 0 {
		return nil, errors.New("host PID broker handler limit must be positive")
	}
	if admissionPermits < 0 || admissionPermits == 1 ||
		admissionPermits > MaximumDriverAdmissionPermits {
		return nil, fmt.Errorf("host PID broker admission permits must be 0 or between 2 and %d",
			MaximumDriverAdmissionPermits)
	}
	if admissionMaxHold <= 0 || admissionMaxHold > MaximumDriverAdmissionMaxHold {
		return nil, fmt.Errorf("host PID broker admission max hold must be positive and at most %s",
			MaximumDriverAdmissionMaxHold)
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
	broker := &Broker{
		listener:               listener,
		socketPath:             socketPath,
		socket:                 identity,
		lockFile:               lockFile,
		handlerSlots:           make(chan struct{}, handlerLimit),
		admissionHandlerBudget: admissionHandlerBudget(handlerLimit),
		admissionMaxHold:       admissionMaxHold,
		shutdown:               make(chan struct{}),
	}
	broker.admission = newDriverAdmissionController(admissionPermits)
	return broker, nil
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
	command, valid := requestCommand(request)
	if !valid {
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
	if command == commandGetPID {
		response := makeResponse(statusOK, uint32(pid))
		writeResponse(connection, response)
		return
	}
	priority, ok := admissionPriority(command)
	if !ok {
		writeResponse(connection, makeResponse(statusInvalidRequest, 0))
		return
	}
	broker.handleDriverAdmission(connection, uint32(pid), priority)
}

func (broker *Broker) handleDriverAdmission(connection *net.UnixConn,
	peerPID uint32, priority driverAdmissionPriority) {
	if broker.admission == nil {
		writeResponse(connection, makeResponse(statusUnavailable, 0))
		return
	}
	handlerBudget := broker.admissionHandlerBudget
	if priority != driverAdmissionRelease {
		// Keep the last slot for release requests so a teardown is never
		// rejected while initialization or retention requests wait.
		handlerBudget--
	}
	if broker.admissionHandlers.Add(1) > int64(handlerBudget) {
		broker.admissionHandlers.Add(-1)
		writeResponse(connection, makeResponse(statusUnavailable, 0))
		return
	}
	defer broker.admissionHandlers.Add(-1)

	if err := connection.SetDeadline(time.Time{}); err != nil {
		return
	}
	connectionDone := make(chan struct{})
	go func() {
		buffer := make([]byte, 1)
		for {
			read, _ := connection.Read(buffer)
			if read > 0 {
				continue
			}
			close(connectionDone)
			return
		}
	}()

	if !broker.admission.acquire(priority, connectionDone, broker.shutdown) {
		return
	}
	defer broker.admission.release(priority)

	if err := connection.SetWriteDeadline(
		time.Now().Add(transactionTimeout)); err != nil {
		return
	}
	granted := time.Now()
	if !writeResponse(connection, makeResponse(statusOK, peerPID)) {
		return
	}
	expired := time.NewTimer(broker.admissionMaxHold)
	defer expired.Stop()
	select {
	case <-connectionDone:
	case <-broker.shutdown:
	case <-expired.C:
		// Fail open: free the permit for other workloads but leave the
		// client and its driver call alone. Returning closes this lease, so
		// a later close from the client cannot release another permit.
		klog.Warningf(
			"Driver admission lease of PID %d expired after %s; releasing its permit",
			peerPID, time.Since(granted).Round(time.Millisecond))
	}
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
	response [responseSize]byte) bool {
	written := 0
	for written < len(response) {
		count, err := connection.Write(response[written:])
		if err != nil || count == 0 {
			return false
		}
		written += count
	}
	return true
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
		if broker.admission != nil {
			broker.admission.close()
		}
		close(broker.shutdown)
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
