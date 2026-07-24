package modbustcp

import (
	"encoding/binary"
	"testing"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/testutil"
)

func TestBuildRequestGoldenVector(t *testing.T) {
	t.Parallel()

	rtu := testutil.ReadHexFixture(t, "modbus_read_rtu.hex")
	got, err := BuildRequest(0x0001, rtu)
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	want := testutil.ReadHexFixture(t, "modbus_tcp_request.hex")
	testutil.AssertBytesEqual(t, got, want)
	testutil.AssertBytesEqual(
		t,
		rtu,
		testutil.ReadHexFixture(t, "modbus_read_rtu.hex"),
	)
}

func TestBuildRequestLimits(t *testing.T) {
	t.Parallel()

	if _, err := BuildRequest(1, []byte{0x01}); err == nil {
		t.Fatal("BuildRequest(short) error = nil")
	}
	if _, err := BuildRequest(1, make([]byte, 0xFFFF+3)); err == nil {
		t.Fatal("BuildRequest(too long) error = nil")
	}
}

func TestParseResponseRebuildsCRC(t *testing.T) {
	t.Parallel()

	response := testutil.ReadHexFixture(t, "modbus_tcp_response.hex")
	got, err := ParseResponse(1, response)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	want := modbus.AppendCRC([]byte{0x01, 0x03, 0x02, 0x00, 0xFF})
	testutil.AssertBytesEqual(t, got, want)
	if !modbus.ValidateCRC(got) {
		t.Fatalf("ParseResponse() returned invalid CRC: %X", got)
	}
}

func TestParseResponseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response []byte
		want     string
	}{
		{
			name:     "too short",
			response: make([]byte, 9),
			want:     "ModBusTCP: Response too short! Length=9",
		},
		{
			name: "wrong transaction",
			response: []byte{
				0x02, 0x00, 0x00, 0x00, 0x00, 0x05,
				0x01, 0x03, 0x02, 0x00, 0xFF,
			},
			want: "ModBusTCP: Wrong TransactionId!",
		},
		{
			name: "declared length exceeds payload",
			response: []byte{
				0x01, 0x00, 0x00, 0x00, 0x00, 0x08,
				0x01, 0x03, 0x02, 0x00, 0xFF,
			},
			want: "ModBusTCP: response length 8 exceeds available payload 5",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseResponse(1, test.response)
			if got != nil {
				t.Fatalf("ParseResponse() = %X, want nil", got)
			}
			if err == nil || err.Error() != test.want {
				t.Fatalf("ParseResponse() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseResponseExceptionTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code byte
		want string
	}{
		{0x01, "Illegal Function"},
		{0x02, "Illegal Data Address"},
		{0x03, "Illegal Data Value"},
		{0x04, "Slave Device Failure"},
		{0x05, "Acknowledge"},
		{0x06, "Slave Device Busy"},
		{0x08, "Memory Parity Error"},
		{0x0A, "Gateway Path Unavailable"},
		{0x0B, "Gateway Target Device Failed to Respond"},
		{0xFF, "??"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()

			response := make([]byte, 10)
			binary.LittleEndian.PutUint16(response[0:2], 1)
			binary.BigEndian.PutUint16(response[4:6], 4)
			response[8] = 0x83
			response[9] = test.code
			got, err := ParseResponse(1, response)
			if got != nil {
				t.Fatalf("ParseResponse() = %X, want nil", got)
			}
			want := "Error response: " + decimalByte(test.code) + "=" + test.want
			if err == nil || err.Error() != want {
				t.Fatalf("ParseResponse() error = %v, want %q", err, want)
			}
		})
	}
}

func decimalByte(value byte) string {
	if value == 0 {
		return "0"
	}
	var result [3]byte
	index := len(result)
	for value > 0 {
		index--
		result[index] = '0' + value%10
		value /= 10
	}
	return string(result[index:])
}
