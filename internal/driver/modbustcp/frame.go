package modbustcp

import (
	"encoding/binary"
	"fmt"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
)

const mbapHeaderLength = 6

// BuildRequest converts a complete RTU frame to the original GbbConnect2 MBAP
// representation. The transaction ID is little-endian for compatibility.
func BuildRequest(transactionID uint16, rtu []byte) ([]byte, error) {
	if len(rtu) < 2 {
		return nil, fmt.Errorf("ModBusTCP: RTU frame too short! Length=%d", len(rtu))
	}
	pduLength := len(rtu) - 2
	if pduLength > 0xFFFF {
		return nil, fmt.Errorf("ModBusTCP: RTU frame too long! Length=%d", len(rtu))
	}

	request := make([]byte, mbapHeaderLength+pduLength)
	binary.LittleEndian.PutUint16(request[0:2], transactionID)
	binary.BigEndian.PutUint16(request[4:6], uint16(pduLength))
	copy(request[mbapHeaderLength:], rtu[:pduLength])
	return request, nil
}

// ParseResponse validates an MBAP response and rebuilds the complete RTU frame.
func ParseResponse(transactionID uint16, response []byte) ([]byte, error) {
	if len(response) < 10 {
		return nil, fmt.Errorf(
			"ModBusTCP: Response too short! Length=%d",
			len(response),
		)
	}
	if binary.LittleEndian.Uint16(response[0:2]) != transactionID {
		//nolint:staticcheck // The compatibility contract requires this exact text.
		return nil, fmt.Errorf("ModBusTCP: Wrong TransactionId!")
	}
	if response[8] > 127 {
		code := response[9]
		//nolint:staticcheck // The compatibility contract requires this exact text.
		return nil, fmt.Errorf("Error response: %d=%s", code, exceptionMessage(code))
	}

	pduLength := int(binary.BigEndian.Uint16(response[4:6]))
	if pduLength > len(response)-mbapHeaderLength {
		return nil, fmt.Errorf(
			"ModBusTCP: response length %d exceeds available payload %d",
			pduLength,
			len(response)-mbapHeaderLength,
		)
	}
	return modbus.AppendCRC(response[mbapHeaderLength : mbapHeaderLength+pduLength]), nil
}

func exceptionMessage(code byte) string {
	switch code {
	case 0x01:
		return "Illegal Function"
	case 0x02:
		return "Illegal Data Address"
	case 0x03:
		return "Illegal Data Value"
	case 0x04:
		return "Slave Device Failure"
	case 0x05:
		return "Acknowledge"
	case 0x06:
		return "Slave Device Busy"
	case 0x08:
		return "Memory Parity Error"
	case 0x0A:
		return "Gateway Path Unavailable"
	case 0x0B:
		return "Gateway Target Device Failed to Respond"
	default:
		return "??"
	}
}
