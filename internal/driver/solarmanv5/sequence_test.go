package solarmanv5

import "testing"

func TestSequenceGeneratorInitialAndWrap(t *testing.T) {
	t.Parallel()

	generator := sequenceGenerator{initial: func() byte { return 0xFF }}
	want := []byte{0xFF, 0x00, 0x01}
	for index, expected := range want {
		if got := generator.next(); got != expected {
			t.Fatalf("next() call %d = %02X, want %02X", index+1, got, expected)
		}
	}
}

func TestSequenceGeneratorRejectsZeroInitial(t *testing.T) {
	t.Parallel()

	generator := sequenceGenerator{initial: func() byte { return 0 }}
	if got := generator.next(); got != 1 {
		t.Fatalf("next() = %d, want 1", got)
	}
}

func TestDefaultSequenceStartsInDocumentedRange(t *testing.T) {
	t.Parallel()

	for range 100 {
		generator := newSequenceGenerator()
		if got := generator.next(); got == 0 {
			t.Fatal("default sequence initialized to zero")
		}
	}
}
