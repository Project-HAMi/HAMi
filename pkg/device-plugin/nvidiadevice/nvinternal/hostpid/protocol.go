/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import "encoding/binary"

const (
	protocolVersion uint16 = 1
	commandGetPID   uint16 = 1

	statusOK             uint16 = 0
	statusInvalidRequest uint16 = 1

	requestSize  = 8
	responseSize = 12
)

var protocolMagic = [4]byte{'H', 'P', 'I', 'D'}

func validRequest(request []byte) bool {
	return len(request) == requestSize &&
		string(request[:4]) == string(protocolMagic[:]) &&
		binary.BigEndian.Uint16(request[4:6]) == protocolVersion &&
		binary.BigEndian.Uint16(request[6:8]) == commandGetPID
}

func makeResponse(status uint16, pid uint32) [responseSize]byte {
	var response [responseSize]byte

	copy(response[:4], protocolMagic[:])
	binary.BigEndian.PutUint16(response[4:6], protocolVersion)
	binary.BigEndian.PutUint16(response[6:8], status)
	binary.BigEndian.PutUint32(response[8:12], pid)
	return response
}
