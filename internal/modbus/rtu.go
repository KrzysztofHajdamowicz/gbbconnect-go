// Package modbus implements Modbus RTU framing and codecs.
package modbus

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

const (
	functionReadHoldingRegisters   byte = 0x03
	functionWriteMultipleRegisters byte = 0x10
	maxReadRegisters                    = 125
	maxWriteBytes                       = 250
)

// CRC16 computes the Modbus CRC over all bytes except the final two-byte CRC
// slot, matching GbbConnect2 ModBus.GetCRC.
func CRC16(data []byte) (lo, hi byte) {
	payloadLength := len(data) - 2
	if payloadLength < 0 {
		payloadLength = 0
	}

	crc := uint16(0xFFFF)
	for _, value := range data[:payloadLength] {
		crc ^= uint16(value)
		for range 8 {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return byte(crc), byte(crc >> 8)
}

// AppendCRC returns a new frame with a little-endian Modbus CRC appended.
func AppendCRC(payload []byte) []byte {
	frame := make([]byte, len(payload)+2)
	copy(frame, payload)
	lo, hi := CRC16(frame)
	frame[len(frame)-2] = lo
	frame[len(frame)-1] = hi
	return frame
}

// ValidateCRC reports whether a complete RTU frame has the expected CRC.
func ValidateCRC(frame []byte) bool {
	if len(frame) < 2 {
		return false
	}
	lo, hi := CRC16(frame)
	return frame[len(frame)-2] == lo && frame[len(frame)-1] == hi
}

// BuildReadHoldingRegisters builds a function 0x03 RTU request. It returns nil
// when count exceeds the Modbus limit of 125 registers.
func BuildReadHoldingRegisters(unit byte, start, count uint16) []byte {
	if count > maxReadRegisters {
		return nil
	}

	payload := make([]byte, 6)
	payload[0] = unit
	payload[1] = functionReadHoldingRegisters
	binary.BigEndian.PutUint16(payload[2:4], start)
	binary.BigEndian.PutUint16(payload[4:6], count)
	return AppendCRC(payload)
}

// BuildWriteMultipleRegisters builds a function 0x10 RTU request. Odd data is
// padded with a trailing zero byte. It returns nil above the 250-byte limit.
func BuildWriteMultipleRegisters(unit byte, start uint16, values []byte) []byte {
	if len(values) > maxWriteBytes {
		return nil
	}

	byteCount := len(values)
	if byteCount%2 != 0 {
		byteCount++
	}
	registerCount := byteCount / 2

	payload := make([]byte, 7+byteCount)
	payload[0] = unit
	payload[1] = functionWriteMultipleRegisters
	binary.BigEndian.PutUint16(payload[2:4], start)
	binary.BigEndian.PutUint16(payload[4:6], uint16(registerCount))
	payload[6] = byte(byteCount)
	copy(payload[7:], values)
	return AppendCRC(payload)
}

// EncodeHex encodes bytes as uppercase hexadecimal without separators.
func EncodeHex(data []byte) string {
	result := make([]byte, hex.EncodedLen(len(data)))
	hex.Encode(result, data)
	for index, value := range result {
		if value >= 'a' && value <= 'f' {
			result[index] = value - ('a' - 'A')
		}
	}
	return string(result)
}

// DecodeHex decodes a case-insensitive, separator-free hexadecimal string.
func DecodeHex(value string) ([]byte, error) {
	if len(value)%2 != 0 {
		return nil, fmt.Errorf("hex string must have even length: %d", len(value))
	}
	result := make([]byte, hex.DecodedLen(len(value)))
	if _, err := hex.Decode(result, []byte(value)); err != nil {
		return nil, fmt.Errorf("decode hex string: %w", err)
	}
	return result, nil
}
