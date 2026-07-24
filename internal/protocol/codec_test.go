package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRequestExample(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"OrderId": "read-batch-001",
		"Lines": [
			{ "LineNo": 1, "Modbus": "0103009C0003C5E5" },
			{ "LineNo": 2, "Modbus": "0103009F0001B424" }
		]
	}`)
	header, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if header == nil {
		t.Fatal("Decode() header = nil")
	}
	if header.OrderID == nil || *header.OrderID != "read-batch-001" {
		t.Errorf("OrderID = %v", header.OrderID)
	}
	if len(header.Lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(header.Lines))
	}
	if header.Lines[0].LineNo != 1 ||
		header.Lines[0].Modbus == nil ||
		*header.Lines[0].Modbus != "0103009C0003C5E5" {
		t.Errorf("line 0 = %+v", header.Lines[0])
	}
	if header.Lines[1].LineNo != 2 ||
		header.Lines[1].Modbus == nil ||
		*header.Lines[1].Modbus != "0103009F0001B424" {
		t.Errorf("line 1 = %+v", header.Lines[1])
	}
}

func TestDecodeProductionShapePreservesZeroAndRequestMetadata(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"OrderId":"order-0",
		"SendLastLog":1,
		"Lines":[{
			"LineNo":0,
			"Tag":"",
			"Timestamp":1784761247,
			"Modbus":"01030204000345b2"
		}]
	}`)
	header, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if header.SendLastLog == nil || *header.SendLastLog != 1 {
		t.Errorf("SendLastLog = %v", header.SendLastLog)
	}
	if len(header.Lines) != 1 || header.Lines[0].LineNo != 0 {
		t.Fatalf("Lines = %+v", header.Lines)
	}
	line := header.Lines[0]
	if line.Tag == nil || *line.Tag != "" {
		t.Errorf("Tag = %v, want pointer to empty string", line.Tag)
	}
	if line.Timestamp == nil || *line.Timestamp != 1784761247 {
		t.Errorf("Timestamp = %v", line.Timestamp)
	}
	if line.Modbus == nil || *line.Modbus != "01030204000345b2" {
		t.Errorf("Modbus = %v, want original input case", line.Modbus)
	}
}

func TestDecodeAllowsTrailingCommas(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"OrderId": "comma,} stays",
		"Unknown": {"nested": true,},
		"Lines": [
			{"LineNo": 0, "Tag": "escaped quote: \",]", "Modbus": "abcd",},
			{"LineNo": 1,},
		],
	}`)
	header, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if header.OrderID == nil || *header.OrderID != "comma,} stays" {
		t.Errorf("OrderID = %v", header.OrderID)
	}
	if len(header.Lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(header.Lines))
	}
	if header.Lines[0].Tag == nil || *header.Lines[0].Tag != `escaped quote: ",]` {
		t.Errorf("Tag = %v", header.Lines[0].Tag)
	}
}

func TestDecodeNullAndWhitespace(t *testing.T) {
	t.Parallel()

	for _, data := range []string{"", " \r\n\t ", "null"} {
		header, err := Decode([]byte(data))
		if err != nil {
			t.Errorf("Decode(%q) error = %v", data, err)
		}
		if header != nil {
			t.Errorf("Decode(%q) = %+v, want nil", data, header)
		}
	}
}

func TestDecodeRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	for _, data := range []string{
		`{"Lines":[}`,
		`{"OrderId":"unterminated}`,
		`{"OrderId":1}`,
		`{"Lines":null,,"OrderId":"x"}`,
	} {
		if _, err := Decode([]byte(data)); err == nil {
			t.Errorf("Decode(%q) error = nil", data)
		}
	}
}

func TestEncodeOmitsNilFieldsAndUsesPascalCase(t *testing.T) {
	t.Parallel()

	orderID := "read-batch-001"
	modbus := "0103060012003400565886"
	version := "1.3.0-go"
	environment := "Linux"
	header := &Header{
		OrderID: &orderID,
		Lines: []Line{{
			LineNo: 1,
			Modbus: &modbus,
		}},
		GBBVersion:     &version,
		GBBEnvironment: &environment,
	}

	data, err := Encode(header)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(encoded) error = %v", err)
	}
	for _, field := range []string{"OrderId", "Lines", "GbbVersion", "GbbEnvironment"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("encoded JSON missing %q: %s", field, data)
		}
	}
	for _, field := range []string{
		"Error",
		"LogLevel",
		"SendLastLog",
		"SubInverterSN",
		"LastLog",
		"orderId",
		"lines",
	} {
		if _, ok := decoded[field]; ok {
			t.Errorf("encoded JSON unexpectedly contains %q: %s", field, data)
		}
	}

	var lines []map[string]json.RawMessage
	if err := json.Unmarshal(decoded["Lines"], &lines); err != nil {
		t.Fatalf("decode Lines error = %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("line count = %d, want 1", len(lines))
	}
	if _, ok := lines[0]["LineNo"]; !ok {
		t.Errorf("line missing LineNo: %s", data)
	}
	for _, omitted := range []string{"Tag", "Timestamp", "Error"} {
		if _, ok := lines[0][omitted]; ok {
			t.Errorf("line unexpectedly contains %q: %s", omitted, data)
		}
	}
}

func TestEncodePreservesExplicitZeroEmptyValuesAndArray(t *testing.T) {
	t.Parallel()

	zero := 0
	empty := ""
	header := &Header{
		OrderID:     &empty,
		SendLastLog: &zero,
		Lines:       []Line{},
		LastLog:     &empty,
	}
	data, err := Encode(header)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	got := string(data)
	for _, fragment := range []string{
		`"OrderId":""`,
		`"SendLastLog":0`,
		`"Lines":[]`,
		`"LastLog":""`,
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("encoded JSON %s missing %s", got, fragment)
		}
	}
}

func TestEncodeUppercasesModbusWithoutMutation(t *testing.T) {
	t.Parallel()

	request := "01aBcDef"
	header := &Header{Lines: []Line{{LineNo: 0, Modbus: &request}}}
	data, err := Encode(header)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if !strings.Contains(string(data), `"Modbus":"01ABCDEF"`) {
		t.Errorf("encoded JSON = %s, want uppercase Modbus", data)
	}
	if request != "01aBcDef" {
		t.Errorf("Encode() mutated input to %q", request)
	}
}

func TestEncodeNilHeader(t *testing.T) {
	t.Parallel()

	data, err := Encode(nil)
	if err != nil {
		t.Fatalf("Encode(nil) error = %v", err)
	}
	if string(data) != "null" {
		t.Errorf("Encode(nil) = %q, want null", data)
	}
}

func TestLogLevelConstants(t *testing.T) {
	t.Parallel()

	if LogLevelOnlyErrors != "OnlyErrors" ||
		LogLevelMin != "Min" ||
		LogLevelMax != "Max" {
		t.Errorf(
			"log level constants = %q/%q/%q",
			LogLevelOnlyErrors,
			LogLevelMin,
			LogLevelMax,
		)
	}
}
