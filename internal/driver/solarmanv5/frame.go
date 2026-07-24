package solarmanv5

import (
	"encoding/binary"
	"fmt"
)

const (
	startByte              = 0xA5
	endByte                = 0x15
	controlCode1           = 0x10
	requestControlCode2    = 0x45
	responseControlCode2   = 0x15
	frameType              = 0x02
	requestRTUOffset       = 26
	responseRTUOffset      = 25
	trailerLength          = 2
	requestPayloadOverhead = 15
	frameLengthOverhead    = 13
)

// CreateFrame wraps a complete Modbus RTU frame in a Solarman V5 request.
func CreateFrame(sequence byte, serial int64, rtu []byte) []byte {
	frame := make([]byte, 28+len(rtu))
	frame[0] = startByte
	binary.LittleEndian.PutUint16(
		frame[1:3],
		uint16(len(rtu)+requestPayloadOverhead),
	)
	frame[3] = controlCode1
	frame[4] = requestControlCode2
	frame[5] = sequence
	frame[6] = 0
	binary.LittleEndian.PutUint32(frame[7:11], uint32(serial))
	frame[11] = frameType
	copy(frame[requestRTUOffset:], rtu)

	checksumOffset := len(frame) - trailerLength
	frame[checksumOffset] = checksum(frame[1:checksumOffset])
	frame[len(frame)-1] = endByte
	return frame
}

// ParseFrame validates a Solarman V5 response and extracts its complete Modbus
// RTU frame. The response checksum is intentionally not validated for
// compatibility with SofarSolar devices and GbbConnect2.
func ParseFrame(sequence byte, serial int64, frame []byte) ([]byte, error) {
	frameLength := len(frame)
	if frameLength < 5 {
		return nil, fmt.Errorf("SolarmanV5: Frame too short: %d", frameLength)
	}

	payloadLength := int(binary.LittleEndian.Uint16(frame[1:3]))
	if frameLength != payloadLength+frameLengthOverhead {
		return nil, fmt.Errorf("SolarmanV5: FrameLen != PayloadLength + 13")
	}
	if frame[0] != startByte {
		return nil, fmt.Errorf("SolarmanV5: Wrong start byte")
	}
	if frame[frameLength-1] != endByte {
		return nil, fmt.Errorf("SolarmanV5: Wrong end byte")
	}
	if frame[3] != controlCode1 || frame[4] != responseControlCode2 {
		return nil, fmt.Errorf("SolarmanV5: Wrong ControlCode")
	}
	if frame[5] != sequence {
		return nil, fmt.Errorf("SolarmanV5: Wrong SequenceNumber")
	}

	var expectedSerial [4]byte
	binary.LittleEndian.PutUint32(expectedSerial[:], uint32(serial))
	if frameLength < 12 ||
		frame[7] != expectedSerial[0] ||
		frame[8] != expectedSerial[1] ||
		frame[9] != expectedSerial[2] ||
		frame[10] != expectedSerial[3] {
		return nil, fmt.Errorf("SolarmanV5: Wrong SerialNumber")
	}
	if frame[11] != frameType {
		return nil, fmt.Errorf("SolarmanV5: Wrong FrameType")
	}
	if frameLength-responseRTUOffset-trailerLength < 5 {
		return nil, fmt.Errorf(
			"SolarmanV5: frame does not contain a valid Modbus RTU frame",
		)
	}

	rtu := make([]byte, frameLength-responseRTUOffset-trailerLength)
	copy(rtu, frame[responseRTUOffset:frameLength-trailerLength])
	return rtu, nil
}

func checksum(data []byte) byte {
	var result byte
	for _, value := range data {
		result += value
	}
	return result
}
