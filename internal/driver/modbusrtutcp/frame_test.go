package modbusrtutcp

import "testing"

func TestExpectedResponseLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want int
	}{
		{name: "empty", data: nil, want: 0},
		{name: "unit only", data: []byte{1}, want: 0},
		{name: "read header partial", data: []byte{1, 3}, want: 0},
		{name: "read", data: []byte{1, 3, 6}, want: 11},
		{name: "read-write multiple", data: []byte{1, 23, 2}, want: 7},
		{name: "write single", data: []byte{1, 6}, want: 8},
		{name: "write multiple", data: []byte{1, 16}, want: 8},
		{name: "exception", data: []byte{1, 0x83}, want: 5},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ExpectedResponseLength(test.data)
			if err != nil {
				t.Fatalf("ExpectedResponseLength() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ExpectedResponseLength() = %d, want %d", got, test.want)
			}
		})
	}
}
