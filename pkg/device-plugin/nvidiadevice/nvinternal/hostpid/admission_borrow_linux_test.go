/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import (
	"net"
	"testing"
	"time"
)

type admissionGrant struct {
	granted  bool
	borrowed bool
}

func acquireGranted(controller *driverAdmissionController,
	priority driverAdmissionPriority, connectionDone <-chan struct{},
	shutdown <-chan struct{}) bool {
	granted, _ := controller.acquire(priority, connectionDone, shutdown)
	return granted
}

// startAdmission requests a permit in a goroutine. A request that still waits
// when the test ends is cancelled through shutdown, so the goroutine exits.
func startAdmission(t *testing.T, controller *driverAdmissionController,
	priority driverAdmissionPriority) <-chan admissionGrant {
	t.Helper()
	shutdown := make(chan struct{})
	done := make(chan struct{})
	result := make(chan admissionGrant, 1)
	go func() {
		defer close(done)
		granted, borrowed := controller.acquire(priority, nil, shutdown)
		result <- admissionGrant{granted, borrowed}
	}()
	t.Cleanup(func() {
		close(shutdown)
		<-done
	})
	return result
}

func waitGranted(t *testing.T, result <-chan admissionGrant) admissionGrant {
	t.Helper()
	select {
	case grant := <-result:
		if !grant.granted {
			t.Fatal("admission was not granted")
		}
		return grant
	case <-time.After(2 * time.Second):
		t.Fatal("admission was not granted in time")
	}
	return admissionGrant{}
}

func controllerState(controller *driverAdmissionController) (bool, int) {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	return controller.releaseActive, controller.borrowedForward
}

func waitQueued(t *testing.T, controller *driverAdmissionController,
	priority driverAdmissionPriority) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		controller.mutex.Lock()
		queued := controller.hasPendingLocked(priority)
		controller.mutex.Unlock()
		if queued {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("priority %d did not queue", priority)
		}
		time.Sleep(time.Millisecond)
	}
}

func expectWaiting(t *testing.T, result <-chan admissionGrant) {
	t.Helper()
	select {
	case grant := <-result:
		t.Fatalf("admission completed while it should wait: %+v", grant)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDriverAdmissionReleaseUsesOwnSlotFirst(t *testing.T) {
	controller := newDriverAdmissionController(4)
	if waitGranted(t, startAdmission(t, controller, driverAdmissionRelease)).borrowed {
		t.Fatal("the first release borrowed a forward permit")
	}
	if releaseActive, _ := controllerState(controller); !releaseActive {
		t.Fatal("the first release did not take the release slot")
	}
}

func TestDriverAdmissionReleaseBorrowsBelowCap(t *testing.T) {
	controller := newDriverAdmissionController(4)
	waitGranted(t, startAdmission(t, controller, driverAdmissionRelease))
	for index := 0; index < 2; index++ {
		if !waitGranted(t, startAdmission(t, controller, driverAdmissionRelease)).borrowed {
			t.Fatal("release did not borrow an idle forward permit")
		}
	}
	expectWaiting(t, startAdmission(t, controller, driverAdmissionRelease))
	waitGranted(t, startAdmission(t, controller, driverAdmissionInit))
	if got := controller.activeCount(); got != 4 {
		t.Fatalf("active admissions=%d, want 4", got)
	}
}

func TestDriverAdmissionTwoPermitsNeverBorrow(t *testing.T) {
	controller := newDriverAdmissionController(2)
	waitGranted(t, startAdmission(t, controller, driverAdmissionRelease))
	release := startAdmission(t, controller, driverAdmissionRelease)
	expectWaiting(t, release)
	waitGranted(t, startAdmission(t, controller, driverAdmissionRetain))
	controller.release(driverAdmissionRetain, false)
	expectWaiting(t, release)
	if _, borrowed := controllerState(controller); borrowed != 0 {
		t.Fatalf("borrowedForward=%d, want 0", borrowed)
	}
}

func TestDriverAdmissionReleaseDoesNotBorrowWhileForwardWaits(t *testing.T) {
	controller := newDriverAdmissionController(3)
	waitGranted(t, startAdmission(t, controller, driverAdmissionRelease))
	waitGranted(t, startAdmission(t, controller, driverAdmissionInit))
	waitGranted(t, startAdmission(t, controller, driverAdmissionInit))
	init := startAdmission(t, controller, driverAdmissionInit)
	waitQueued(t, controller, driverAdmissionInit)
	release := startAdmission(t, controller, driverAdmissionRelease)
	waitQueued(t, controller, driverAdmissionRelease)

	controller.release(driverAdmissionInit, false)
	waitGranted(t, init)
	expectWaiting(t, release)
}

func TestDriverAdmissionBorrowedPermitReturnsToForwardPool(t *testing.T) {
	controller := newDriverAdmissionController(3)
	waitGranted(t, startAdmission(t, controller, driverAdmissionRelease))
	if !waitGranted(t, startAdmission(t, controller, driverAdmissionRelease)).borrowed {
		t.Fatal("release did not borrow an idle forward permit")
	}
	waitGranted(t, startAdmission(t, controller, driverAdmissionInit))
	init := startAdmission(t, controller, driverAdmissionInit)
	expectWaiting(t, init)

	controller.release(driverAdmissionRelease, true)
	waitGranted(t, init)
	if releaseActive, borrowed := controllerState(controller); !releaseActive || borrowed != 0 {
		t.Fatalf("releaseActive=%v borrowedForward=%d, want true and 0",
			releaseActive, borrowed)
	}
}

func TestDriverAdmissionInitNeverTakesReleaseSlot(t *testing.T) {
	controller := newDriverAdmissionController(2)
	waitGranted(t, startAdmission(t, controller, driverAdmissionInit))
	expectWaiting(t, startAdmission(t, controller, driverAdmissionInit))
	if releaseActive, _ := controllerState(controller); releaseActive {
		t.Fatal("init took the release slot")
	}
}

func TestBrokerBorrowedReleaseReturnsForwardPermit(t *testing.T) {
	broker, socketPath := startTestBrokerWithAdmission(t, 3)
	acquire := func(command uint16) net.Conn {
		t.Helper()
		connection, status, _, err := acquireDriverAdmissionCommand(
			socketPath, time.Second, command)
		if err != nil || status != statusOK {
			t.Fatalf("command %d: status=%d err=%v", command, status, err)
		}
		return connection
	}
	ownSlot := acquire(commandAcquireDriverRelease)
	defer ownSlot.Close()
	borrowed := acquire(commandAcquireDriverRelease)
	defer borrowed.Close()
	initLease := acquire(commandAcquireDriverInit)
	defer initLease.Close()
	waitForActiveAdmissions(t, broker, 3)

	type result struct {
		connection net.Conn
		status     uint16
		err        error
	}
	waiting := make(chan result, 1)
	go func() {
		connection, status, _, err := acquireDriverAdmissionCommand(
			socketPath, 2*time.Second, commandAcquireDriverInit)
		waiting <- result{connection, status, err}
	}()
	select {
	case got := <-waiting:
		if got.connection != nil {
			_ = got.connection.Close()
		}
		t.Fatalf("init did not wait: status=%d err=%v", got.status, got.err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-waiting:
		if got.err != nil || got.status != statusOK {
			t.Fatalf("init after borrowed release: status=%d err=%v",
				got.status, got.err)
		}
		_ = got.connection.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("closing a borrowed release did not free a forward permit")
	}
	if releaseActive, _ := controllerState(broker.admission); !releaseActive {
		t.Fatal("closing a borrowed release freed the release slot")
	}
}
