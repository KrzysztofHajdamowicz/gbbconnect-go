package testutil

import (
	"fmt"
	"strings"
	"testing"
)

func TestReadFixtures(t *testing.T) {
	t.Parallel()

	got := ReadHexFixture(t, "modbus_read_rtu.hex")
	AssertBytesEqual(
		t,
		got,
		[]byte{0x01, 0x03, 0x00, 0x9C, 0x00, 0x03, 0xC5, 0xE5},
	)

	first := ReadFixture(t, "protocol_request.json")
	first[0] = 0
	second := ReadFixture(t, "protocol_request.json")
	if second[0] == 0 {
		t.Fatal("ReadFixture() returned shared mutable storage")
	}
}

func TestAssertBytesEqualDiff(t *testing.T) {
	t.Parallel()

	recorder := &fatalRecorder{}
	AssertBytesEqual(recorder, []byte{0x01, 0xFF}, []byte{0x01, 0x02, 0x03})
	for _, want := range []string{
		"offset 0x1",
		"got (2): 01 FF",
		"want (3): 01 02 03",
	} {
		if !strings.Contains(recorder.message, want) {
			t.Errorf("failure %q does not contain %q", recorder.message, want)
		}
	}
}

type fatalRecorder struct {
	message string
}

func (*fatalRecorder) Helper() {}

func (recorder *fatalRecorder) Fatalf(format string, args ...any) {
	recorder.message = strings.TrimSpace(fmt.Sprintf(format, args...))
}
