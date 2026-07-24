package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ResponseKind classifies a successfully parsed Modbus response.
type ResponseKind uint8

const (
	// ResponseUnknown is returned when a response cannot be parsed.
	ResponseUnknown ResponseKind = iota
	// ResponseRead contains only the byte-count-delimited read data.
	ResponseRead
	// ResponseWrite contains the complete RTU response frame.
	ResponseWrite
)

// ErrWrongCRC matches the error text produced by the GbbConnect2 local
// read/write path.
//
//nolint:staticcheck // The compatibility contract requires this exact text.
var ErrWrongCRC = errors.New("Wrong CRC!")

// ParseResponse validates and interprets a complete Modbus RTU response.
//
// Functions 5 and above are classified as writes, except function 23, whose
// response contains read data. Exception responses are returned as errors.
func ParseResponse(rtu []byte) (ResponseKind, []byte, error) {
	if !ValidateCRC(rtu) {
		return ResponseUnknown, nil, ErrWrongCRC
	}
	if len(rtu) < 4 {
		return ResponseUnknown, nil, fmt.Errorf("malformed response: length %d", len(rtu))
	}

	function := rtu[1]
	if function > 128 {
		if len(rtu) < 5 {
			return ResponseUnknown, nil, fmt.Errorf(
				"malformed exception response: length %d",
				len(rtu),
			)
		}
		//nolint:staticcheck // The compatibility contract requires this exact text.
		return ResponseUnknown, nil, fmt.Errorf(
			"Error response: function: %d, error=%d",
			function-128,
			rtu[2],
		)
	}

	if function >= 5 && function != 23 {
		return ResponseWrite, rtu, nil
	}

	if len(rtu) < 5 {
		return ResponseUnknown, nil, fmt.Errorf("malformed read response: length %d", len(rtu))
	}
	byteCount := int(rtu[2])
	dataEnd := 3 + byteCount
	if dataEnd != len(rtu)-2 {
		return ResponseUnknown, nil, fmt.Errorf(
			"malformed read response: byte count %d does not match frame length %d",
			byteCount,
			len(rtu),
		)
	}

	data := make([]byte, byteCount)
	copy(data, rtu[3:dataEnd])
	return ResponseRead, data, nil
}

// DecodeRegisters decodes a read response's data region as big-endian uint16
// register values.
func DecodeRegisters(data []byte) ([]uint16, error) {
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("register data must have even length: %d", len(data))
	}

	registers := make([]uint16, len(data)/2)
	for index := range registers {
		offset := index * 2
		registers[index] = binary.BigEndian.Uint16(data[offset : offset+2])
	}
	return registers, nil
}
