package solarmanv5

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/testutil"
)

const testSerial int64 = 0x12345678

func TestCreateFrameGoldenVector(t *testing.T) {
	t.Parallel()

	rtu := testutil.ReadHexFixture(t, "modbus_read_rtu.hex")
	got := CreateFrame(0x2A, testSerial, rtu)
	want := testutil.ReadHexFixture(t, "solarman_v5_request.hex")
	testutil.AssertBytesEqual(t, got, want)
	testutil.AssertBytesEqual(
		t,
		rtu,
		testutil.ReadHexFixture(t, "modbus_read_rtu.hex"),
	)
}

func TestParseFrameExtractsRTUAndIgnoresChecksum(t *testing.T) {
	t.Parallel()

	rtu := modbus.AppendCRC([]byte{0x01, 0x03, 0x02, 0x00, 0xFF})
	frame := testutil.ReadHexFixture(t, "solarman_v5_response.hex")
	frame[len(frame)-2] ^= 0xFF

	got, err := ParseFrame(0x2A, testSerial, frame)
	if err != nil {
		t.Fatalf("ParseFrame() error = %v", err)
	}
	testutil.AssertBytesEqual(t, got, rtu)

	frame[responseRTUOffset] ^= 0xFF
	if bytes.Equal(got, frame[responseRTUOffset:len(frame)-2]) {
		t.Fatal("ParseFrame() returned data backed by the input frame")
	}
}

func TestParseFrameValidationErrors(t *testing.T) {
	t.Parallel()

	rtu := modbus.AppendCRC([]byte{0x01, 0x03, 0x02, 0x12, 0x34})
	valid := buildResponseFrame(0x2A, testSerial, rtu)

	tests := []struct {
		name   string
		frame  []byte
		mutate func([]byte)
		want   string
	}{
		{
			name:  "too short",
			frame: []byte{0xA5, 0x00, 0x00, 0x10},
			want:  "SolarmanV5: Frame too short: 4",
		},
		{
			name: "length mismatch",
			mutate: func(frame []byte) {
				frame[1]++
			},
			want: "SolarmanV5: FrameLen != PayloadLength + 13",
		},
		{
			name: "wrong start",
			mutate: func(frame []byte) {
				frame[0] = 0
			},
			want: "SolarmanV5: Wrong start byte",
		},
		{
			name: "wrong end",
			mutate: func(frame []byte) {
				frame[len(frame)-1] = 0
			},
			want: "SolarmanV5: Wrong end byte",
		},
		{
			name: "wrong control",
			mutate: func(frame []byte) {
				frame[4] = requestControlCode2
			},
			want: "SolarmanV5: Wrong ControlCode",
		},
		{
			name: "wrong sequence",
			mutate: func(frame []byte) {
				frame[5]++
			},
			want: "SolarmanV5: Wrong SequenceNumber",
		},
		{
			name: "wrong serial",
			mutate: func(frame []byte) {
				frame[7]++
			},
			want: "SolarmanV5: Wrong SerialNumber",
		},
		{
			name: "wrong frame type",
			mutate: func(frame []byte) {
				frame[11] = 0
			},
			want: "SolarmanV5: Wrong FrameType",
		},
		{
			name:  "missing RTU",
			frame: buildResponseFrame(0x2A, testSerial, []byte{1, 2, 3, 4}),
			want:  "SolarmanV5: frame does not contain a valid Modbus RTU frame",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			frame := bytes.Clone(valid)
			if test.frame != nil {
				frame = bytes.Clone(test.frame)
			}
			if test.mutate != nil {
				test.mutate(frame)
			}
			got, err := ParseFrame(0x2A, testSerial, frame)
			if got != nil {
				t.Fatalf("ParseFrame() = %X, want nil", got)
			}
			if err == nil || err.Error() != test.want {
				t.Fatalf("ParseFrame() error = %v, want %q", err, test.want)
			}
		})
	}
}

func buildResponseFrame(sequence byte, serial int64, rtu []byte) []byte {
	frame := make([]byte, responseRTUOffset+len(rtu)+trailerLength)
	frame[0] = startByte
	binary.LittleEndian.PutUint16(
		frame[1:3],
		uint16(len(frame)-frameLengthOverhead),
	)
	frame[3] = controlCode1
	frame[4] = responseControlCode2
	frame[5] = sequence
	binary.LittleEndian.PutUint32(frame[7:11], uint32(serial))
	frame[11] = frameType
	copy(frame[responseRTUOffset:], rtu)
	frame[len(frame)-2] = checksum(frame[1 : len(frame)-2])
	frame[len(frame)-1] = endByte
	return frame
}
