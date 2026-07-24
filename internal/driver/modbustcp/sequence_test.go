package modbustcp

import "testing"

func TestSequenceGeneratorInitialAndWrap(t *testing.T) {
	t.Parallel()

	generator := sequenceGenerator{initial: func() uint16 { return 0xFFFF }}
	want := []uint16{0xFFFF, 0x0000, 0x0001}
	for index, expected := range want {
		if got := generator.next(); got != expected {
			t.Fatalf("next() call %d = %04X, want %04X", index+1, got, expected)
		}
	}
}

func TestDefaultSequenceStartsNonzero(t *testing.T) {
	t.Parallel()

	for range 100 {
		generator := newSequenceGenerator()
		if got := generator.next(); got == 0 {
			t.Fatal("default sequence initialized to zero")
		}
	}
}
