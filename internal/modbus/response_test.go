package modbus

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestParseResponseRead(t *testing.T) {
	t.Parallel()

	frame := AppendCRC([]byte{
		0x01, 0x03, 0x06,
		0x00, 0x12, 0x00, 0x34, 0x00, 0x56,
	})
	kind, data, err := ParseResponse(frame)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if kind != ResponseRead {
		t.Fatalf("ParseResponse() kind = %v, want %v", kind, ResponseRead)
	}
	if !bytes.Equal(data, []byte{0x00, 0x12, 0x00, 0x34, 0x00, 0x56}) {
		t.Fatalf("ParseResponse() data = %X", data)
	}

	registers, err := DecodeRegisters(data)
	if err != nil {
		t.Fatalf("DecodeRegisters() error = %v", err)
	}
	if want := []uint16{0x0012, 0x0034, 0x0056}; !reflect.DeepEqual(registers, want) {
		t.Fatalf("DecodeRegisters() = %X, want %X", registers, want)
	}

	frame[3] = 0xFF
	if data[0] != 0x00 {
		t.Fatal("ParseResponse() returned read data backed by the input frame")
	}
}

func TestParseResponseWriteReturnsWholeFrame(t *testing.T) {
	t.Parallel()

	for _, function := range []byte{0x05, 0x06, 0x10, 0x18} {
		function := function
		t.Run(EncodeHex([]byte{function}), func(t *testing.T) {
			t.Parallel()

			frame := AppendCRC([]byte{0x01, function, 0x00, 0x10, 0x00, 0x02})
			kind, data, err := ParseResponse(frame)
			if err != nil {
				t.Fatalf("ParseResponse() error = %v", err)
			}
			if kind != ResponseWrite {
				t.Fatalf("ParseResponse() kind = %v, want %v", kind, ResponseWrite)
			}
			if !bytes.Equal(data, frame) {
				t.Fatalf("ParseResponse() data = %X, want %X", data, frame)
			}
			if &data[0] != &frame[0] {
				t.Fatal("ParseResponse() copied a write response")
			}
		})
	}
}

func TestParseResponseFunction23IsRead(t *testing.T) {
	t.Parallel()

	frame := AppendCRC([]byte{0x01, 0x17, 0x02, 0x12, 0x34})
	kind, data, err := ParseResponse(frame)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if kind != ResponseRead {
		t.Fatalf("ParseResponse() kind = %v, want %v", kind, ResponseRead)
	}
	if !bytes.Equal(data, []byte{0x12, 0x34}) {
		t.Fatalf("ParseResponse() data = %X", data)
	}
}

func TestParseResponseExceptionExactError(t *testing.T) {
	t.Parallel()

	frame := AppendCRC([]byte{0x01, 0x83, 0x02})
	kind, data, err := ParseResponse(frame)
	if kind != ResponseUnknown {
		t.Fatalf("ParseResponse() kind = %v, want %v", kind, ResponseUnknown)
	}
	if data != nil {
		t.Fatalf("ParseResponse() data = %X, want nil", data)
	}
	if err == nil || err.Error() != "Error response: function: 3, error=2" {
		t.Fatalf("ParseResponse() error = %v", err)
	}
}

func TestParseResponseWrongCRCFirst(t *testing.T) {
	t.Parallel()

	tests := [][]byte{
		nil,
		{0x01},
		{0x01, 0x03, 0x02, 0x00, 0x01, 0x00, 0x00},
	}
	for _, frame := range tests {
		kind, data, err := ParseResponse(frame)
		if kind != ResponseUnknown || data != nil || !errors.Is(err, ErrWrongCRC) {
			t.Fatalf(
				"ParseResponse(%X) = (%v, %X, %v), want unknown, nil, ErrWrongCRC",
				frame,
				kind,
				data,
				err,
			)
		}
		if err.Error() != "Wrong CRC!" {
			t.Fatalf("ParseResponse(%X) error = %q", frame, err)
		}
	}
}

func TestParseResponseRejectsMalformedReadLength(t *testing.T) {
	t.Parallel()

	frame := AppendCRC([]byte{0x01, 0x03, 0x04, 0x12, 0x34})
	kind, data, err := ParseResponse(frame)
	if kind != ResponseUnknown || data != nil {
		t.Fatalf("ParseResponse() = (%v, %X, %v)", kind, data, err)
	}
	if err == nil {
		t.Fatal("ParseResponse() error = nil")
	}
}

func TestDecodeRegisters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want []uint16
	}{
		{name: "empty", data: nil, want: []uint16{}},
		{name: "boundaries", data: []byte{0x00, 0x00, 0xFF, 0xFF}, want: []uint16{0, 0xFFFF}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := DecodeRegisters(test.data)
			if err != nil {
				t.Fatalf("DecodeRegisters() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("DecodeRegisters() = %v, want %v", got, test.want)
			}
		})
	}

	if _, err := DecodeRegisters([]byte{0x01}); err == nil {
		t.Fatal("DecodeRegisters(odd data) error = nil")
	}
}
