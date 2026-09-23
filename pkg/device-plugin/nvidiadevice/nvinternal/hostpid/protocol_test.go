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
		"command":      {'H', 'P', 'I', 'D', 0, 1, 0, 2},
		"zero command": {'H', 'P', 'I', 'D', 0, 1, 0, 0},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			if validRequest(request) {
				t.Fatal("invalid request was accepted")
			}
		})
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
