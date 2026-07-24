package protocol

import (
	"encoding/json"
	"strings"
)

const (
	// LogLevelOnlyErrors disables informational and driver trace logging.
	LogLevelOnlyErrors = "OnlyErrors"
	// LogLevelMin enables informational logging without driver traces.
	LogLevelMin = "Min"
	// LogLevelMax enables informational logging and both driver traces.
	LogLevelMax = "Max"
)

// Header is the top-level GbbOptimizer request and response object.
type Header struct {
	Error          *string `json:"Error,omitempty"`
	OrderID        *string `json:"OrderId,omitempty"`
	LogLevel       *string `json:"LogLevel,omitempty"`
	SendLastLog    *int    `json:"SendLastLog,omitempty"`
	SubInverterSN  *string `json:"SubInverterSN,omitempty"`
	Lines          []Line  `json:"Lines,omitzero"`
	GBBVersion     *string `json:"GbbVersion,omitempty"`
	GBBEnvironment *string `json:"GbbEnvironment,omitempty"`
	LastLog        *string `json:"LastLog,omitempty"`
}

// Line is one Modbus request or response within a Header.
type Line struct {
	LineNo    int     `json:"LineNo"`
	Tag       *string `json:"Tag,omitempty"`
	Timestamp *int64  `json:"Timestamp,omitempty"`
	Modbus    *string `json:"Modbus,omitempty"`
	Error     *string `json:"Error,omitempty"`
}

// MarshalJSON keeps Modbus hex uppercase without mutating the caller's Line.
func (line Line) MarshalJSON() ([]byte, error) {
	type plain Line
	encoded := plain(line)
	if line.Modbus != nil {
		modbus := strings.ToUpper(*line.Modbus)
		encoded.Modbus = &modbus
	}
	return json.Marshal(encoded)
}
