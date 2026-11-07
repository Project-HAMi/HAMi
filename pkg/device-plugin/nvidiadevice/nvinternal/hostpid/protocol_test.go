/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import (
	"encoding/binary"
	"testing"
)

func TestValidRequest(t *testing.T) {
	valid := []byte{'H', 'P', 'I', 'D', 0, 1, 0, 1}
	if !validRequest(valid) {
		t.Fatal("valid request was rejected")
	}

	tests := map[string][]byte{
		"short":        valid[:7],
		"long":         append(append([]byte{}, valid...), 0),
		"magic":        {'B', 'A', 'D', '!', 0, 1, 0, 1},
		"version":      {'H', 'P', 'I', 'D', 0, 2, 0, 1},
		"command":      {'H', 'P', 'I', 'D', 0, 1, 0, 5},
		"zero command": {'H', 'P', 'I', 'D', 0, 1, 0, 0},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			if validRequest(request) {
				t.Fatal("invalid request was accepted")
			}
		})
	}

	for _, expected := range []uint16{
		commandAcquireDriverInit,
		commandAcquireDriverRetain,
		commandAcquireDriverRelease,
	} {
		admission := []byte{'H', 'P', 'I', 'D', 0, 1,
			byte(expected >> 8), byte(expected)}
		command, ok := requestCommand(admission)
		if !ok || command != expected {
			t.Fatalf("admission request command=%d valid=%v", command, ok)
		}
	}
}

func TestMakeResponse(t *testing.T) {
	response := makeResponse(statusOK, 0x01020304)

	if string(response[:4]) != "HPID" {
		t.Fatalf("unexpected magic %q", response[:4])
	}
	if got := binary.BigEndian.Uint16(response[4:6]); got != protocolVersion {
		t.Fatalf("unexpected version %d", got)
	}
	if got := binary.BigEndian.Uint16(response[6:8]); got != statusOK {
		t.Fatalf("unexpected status %d", got)
	}
	if got := binary.BigEndian.Uint32(response[8:12]); got != 0x01020304 {
		t.Fatalf("unexpected PID %#x", got)
	}
}

func TestMakeErrorResponse(t *testing.T) {
	response := makeResponse(statusInvalidRequest, 0)
	if got := binary.BigEndian.Uint16(response[6:8]); got != statusInvalidRequest {
		t.Fatalf("unexpected status %d", got)
	}
	if got := binary.BigEndian.Uint32(response[8:12]); got != 0 {
		t.Fatalf("unexpected PID %d", got)
	}
}

func TestParseDriverAdmissionPermits(t *testing.T) {
	tests := map[string]struct {
		value     string
		permits   int
		wantError bool
	}{
		"unset":     {value: "", permits: 0},
		"disabled":  {value: "0", permits: 0},
		"one":       {value: "1", wantError: true},
		"enabled":   {value: "4", permits: 4},
		"maximum":   {value: "64", permits: 64},
		"negative":  {value: "-1", wantError: true},
		"too large": {value: "65", wantError: true},
		"text":      {value: "four", wantError: true},
		"space":     {value: " 4", wantError: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			permits, err := ParseDriverAdmissionPermits(test.value)
			if (err != nil) != test.wantError || permits != test.permits {
				t.Fatalf("permits=%d err=%v", permits, err)
			}
		})
	}
}
