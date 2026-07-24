package modbusrtutcp

import "fmt"

// ExpectedResponseLength returns the complete RTU response length once enough
// header bytes are available. A zero length means more bytes are required.
func ExpectedResponseLength(data []byte) (int, error) {
	if len(data) < 2 {
		return 0, nil
	}

	function := data[1]
	if function&0x80 != 0 {
		return 5, nil
	}
	if function >= 5 && function != 23 {
		return 8, nil
	}
	if len(data) < 3 {
		return 0, nil
	}

	length := 3 + int(data[2]) + 2
	if length > maxFrameSize {
		return 0, fmt.Errorf("modbus RTU response is too long: %d", length)
	}
	return length, nil
}
