package modbus

import (
	"bytes"
	"testing"
	"testing/quick"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/testutil"
)

func TestCRC16GoldenVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		frame []byte
		lo    byte
		hi    byte
	}{
		{
			name:  "documented read",
			frame: []byte{0x01, 0x03, 0x00, 0x9C, 0x00, 0x03, 0x00, 0x00},
			lo:    0xC5,
			hi:    0xE5,
		},
		{
			name:  "common ten-register read",
			frame: []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x0A, 0x00, 0x00},
			lo:    0xC5,
			hi:    0xCD,
		},
		{
			name:  "captured inverter read",
			frame: []byte{0x01, 0x03, 0x02, 0x04, 0x00, 0x03, 0x00, 0x00},
			lo:    0x45,
			hi:    0xB2,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			lo, hi := CRC16(test.frame)
			if lo != test.lo || hi != test.hi {
				t.Fatalf("CRC16() = %02X %02X, want %02X %02X", lo, hi, test.lo, test.hi)
			}
		})
	}
}

func TestBuildReadHoldingRegistersGoldenVector(t *testing.T) {
	t.Parallel()

	got := BuildReadHoldingRegisters(1, 0x009C, 3)
	want := testutil.ReadHexFixture(t, "modbus_read_rtu.hex")
	testutil.AssertBytesEqual(t, got, want)
	if !ValidateCRC(got) {
		t.Fatal("BuildReadHoldingRegisters() returned an invalid CRC")
	}
}

func TestBuildReadHoldingRegistersLimit(t *testing.T) {
	t.Parallel()

	if got := BuildReadHoldingRegisters(1, 0, 125); got == nil {
		t.Fatal("BuildReadHoldingRegisters(125) = nil")
	}
	if got := BuildReadHoldingRegisters(1, 0, 126); got != nil {
		t.Fatalf("BuildReadHoldingRegisters(126) = %X, want nil", got)
	}
}

func TestBuildWriteMultipleRegistersGoldenVector(t *testing.T) {
	t.Parallel()

	got := BuildWriteMultipleRegisters(1, 0x0001, []byte{0x00, 0x0A, 0x01, 0x02})
	want := []byte{
		0x01, 0x10, 0x00, 0x01, 0x00, 0x02, 0x04,
		0x00, 0x0A, 0x01, 0x02, 0x92, 0x30,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("BuildWriteMultipleRegisters() = %X, want %X", got, want)
	}
	if !ValidateCRC(got) {
		t.Fatal("BuildWriteMultipleRegisters() returned an invalid CRC")
	}
}

func TestBuildWriteMultipleRegistersPadsOddData(t *testing.T) {
	t.Parallel()

	values := []byte{0x12, 0x34, 0x56}
	got := BuildWriteMultipleRegisters(1, 0x0010, values)
	want := []byte{
		0x01, 0x10, 0x00, 0x10, 0x00, 0x02, 0x04,
		0x12, 0x34, 0x56, 0x00, 0x89, 0xB5,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("BuildWriteMultipleRegisters() = %X, want %X", got, want)
	}
	if !bytes.Equal(values, []byte{0x12, 0x34, 0x56}) {
		t.Fatalf("BuildWriteMultipleRegisters() mutated values: %X", values)
	}
}

func TestBuildWriteMultipleRegistersLimit(t *testing.T) {
	t.Parallel()

	if got := BuildWriteMultipleRegisters(1, 0, make([]byte, 250)); got == nil {
		t.Fatal("BuildWriteMultipleRegisters(250 bytes) = nil")
	}
	if got := BuildWriteMultipleRegisters(1, 0, make([]byte, 251)); got != nil {
		t.Fatalf("BuildWriteMultipleRegisters(251 bytes) length = %d, want nil", len(got))
	}
}

func TestAppendAndValidateCRC(t *testing.T) {
	t.Parallel()

	payload := []byte{0x01, 0x03, 0x00, 0x9C, 0x00, 0x03}
	frame := AppendCRC(payload)
	if !bytes.Equal(frame, []byte{0x01, 0x03, 0x00, 0x9C, 0x00, 0x03, 0xC5, 0xE5}) {
		t.Fatalf("AppendCRC() = %X", frame)
	}
	if !ValidateCRC(frame) {
		t.Fatal("ValidateCRC(AppendCRC(payload)) = false")
	}
	if !bytes.Equal(payload, []byte{0x01, 0x03, 0x00, 0x9C, 0x00, 0x03}) {
		t.Fatalf("AppendCRC() mutated payload: %X", payload)
	}

	frame[2] ^= 0x01
	if ValidateCRC(frame) {
		t.Fatal("ValidateCRC(corrupted frame) = true")
	}
	if ValidateCRC(nil) || ValidateCRC([]byte{0xFF}) {
		t.Fatal("ValidateCRC accepted a frame that is too short")
	}
}

func TestAppendCRCProperty(t *testing.T) {
	t.Parallel()

	property := func(payload []byte) bool {
		return ValidateCRC(AppendCRC(payload))
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("CRC property failed: %v", err)
	}
}

func TestHexGoldenVectorAndErrors(t *testing.T) {
	t.Parallel()

	frame := testutil.ReadHexFixture(t, "modbus_read_rtu.hex")
	const encoded = "0103009C0003C5E5"
	if got := EncodeHex(frame); got != encoded {
		t.Fatalf("EncodeHex() = %q, want %q", got, encoded)
	}

	for _, input := range []string{encoded, "0103009c0003c5e5"} {
		decoded, err := DecodeHex(input)
		if err != nil {
			t.Fatalf("DecodeHex(%q) error = %v", input, err)
		}
		testutil.AssertBytesEqual(t, decoded, frame)
	}

	if _, err := DecodeHex("ABC"); err == nil {
		t.Fatal("DecodeHex(odd length) error = nil")
	}
	if _, err := DecodeHex("GG"); err == nil {
		t.Fatal("DecodeHex(non-hex) error = nil")
	}
}

func FuzzHexRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x01, 0x7F, 0x80, 0xFF})
	f.Add([]byte{0x01, 0x03, 0x00, 0x9C, 0x00, 0x03, 0xC5, 0xE5})

	f.Fuzz(func(t *testing.T, input []byte) {
		encoded := EncodeHex(input)
		decoded, err := DecodeHex(encoded)
		if err != nil {
			t.Fatalf("DecodeHex(EncodeHex()) error = %v", err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("round-trip = %X, want %X", decoded, input)
		}
	})
}
