/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import "encoding/binary"

const (
	protocolVersion             uint16 = 1
	commandGetPID               uint16 = 1
	commandAcquireDriverInit    uint16 = 2
	commandAcquireDriverRetain  uint16 = 3
	commandAcquireDriverRelease uint16 = 4

	statusOK             uint16 = 0
	statusInvalidRequest uint16 = 1
	statusUnavailable    uint16 = 2

	requestSize  = 8
	responseSize = 12
)

var protocolMagic = [4]byte{'H', 'P', 'I', 'D'}

func requestCommand(request []byte) (uint16, bool) {
	if len(request) != requestSize ||
		string(request[:4]) != string(protocolMagic[:]) ||
		binary.BigEndian.Uint16(request[4:6]) != protocolVersion {
		return 0, false
	}
	command := binary.BigEndian.Uint16(request[6:8])
	return command, command == commandGetPID ||
		command == commandAcquireDriverInit ||
		command == commandAcquireDriverRetain ||
		command == commandAcquireDriverRelease
}

func validRequest(request []byte) bool {
	_, valid := requestCommand(request)
	return valid
}

func makeResponse(status uint16, value uint32) [responseSize]byte {
	var response [responseSize]byte

	copy(response[:4], protocolMagic[:])
	binary.BigEndian.PutUint16(response[4:6], protocolVersion)
	binary.BigEndian.PutUint16(response[6:8], status)
	binary.BigEndian.PutUint32(response[8:12], value)
	return response
}
