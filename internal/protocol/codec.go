package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Decode parses a cloud Header. It accepts the trailing object and array
// commas allowed by the original System.Text.Json configuration.
func Decode(data []byte) (*Header, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var header *Header
	if err := json.Unmarshal(removeTrailingCommas(data), &header); err != nil {
		return nil, fmt.Errorf("decode cloud protocol header: %w", err)
	}
	return header, nil
}

// Encode serializes a cloud Header with exact PascalCase names and omitted nil
// fields.
func Encode(header *Header) ([]byte, error) {
	data, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("encode cloud protocol header: %w", err)
	}
	return data, nil
}

func removeTrailingCommas(data []byte) []byte {
	result := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for index, current := range data {
		if inString {
			result = append(result, current)
			switch {
			case escaped:
				escaped = false
			case current == '\\':
				escaped = true
			case current == '"':
				inString = false
			}
			continue
		}

		if current == '"' {
			inString = true
			result = append(result, current)
			continue
		}
		if current != ',' {
			result = append(result, current)
			continue
		}

		next := index + 1
		for next < len(data) && isJSONWhitespace(data[next]) {
			next++
		}
		if next < len(data) && (data[next] == '}' || data[next] == ']') {
			continue
		}
		result = append(result, current)
	}
	return result
}

func isJSONWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}
