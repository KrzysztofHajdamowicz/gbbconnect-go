// Package testutil provides shared protocol fixtures and test assertions.
package testutil

import (
	"bytes"
	"embed"
	"encoding/hex"
	"strings"
)

//go:embed testdata/*
var fixtures embed.FS

type testingTB interface {
	Helper()
	Fatalf(format string, args ...any)
}

// ReadFixture returns an independent copy of a shared fixture.
func ReadFixture(testing testingTB, name string) []byte {
	testing.Helper()

	data, err := fixtures.ReadFile("testdata/" + name)
	if err != nil {
		testing.Fatalf("read fixture %q: %v", name, err)
	}
	return bytes.Clone(data)
}

// ReadHexFixture decodes a whitespace-separated hexadecimal fixture. Text
// following a # character on each line is treated as a comment.
func ReadHexFixture(testing testingTB, name string) []byte {
	testing.Helper()

	data := ReadFixture(testing, name)
	var encoded strings.Builder
	for line := range strings.Lines(string(data)) {
		line, _, _ = strings.Cut(line, "#")
		for field := range strings.FieldsSeq(line) {
			encoded.WriteString(field)
		}
	}

	decoded, err := hex.DecodeString(encoded.String())
	if err != nil {
		testing.Fatalf("decode hex fixture %q: %v", name, err)
	}
	return decoded
}

// AssertBytesEqual reports the first mismatching offset and both complete byte
// sequences, formatted as space-separated uppercase hexadecimal.
func AssertBytesEqual(testing testingTB, got, want []byte) {
	testing.Helper()

	if bytes.Equal(got, want) {
		return
	}
	limit := min(len(got), len(want))
	offset := limit
	for index := range limit {
		if got[index] != want[index] {
			offset = index
			break
		}
	}
	testing.Fatalf(
		"byte mismatch at offset 0x%X\n got (%d): % X\nwant (%d): % X",
		offset,
		len(got),
		got,
		len(want),
		want,
	)
}
