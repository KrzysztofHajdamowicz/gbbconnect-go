package driver

import (
	"reflect"
	"testing"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/modbusrtutcp"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/modbusserial"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/modbustcp"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/random"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/driver/solarmanv5"
)

func TestNewSelectsTransport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		driver config.DriverType
		want   any
	}{
		{driver: config.DriverSolarmanV5, want: (*solarmanv5.Transport)(nil)},
		{driver: config.DriverModbusTCP, want: (*modbustcp.Transport)(nil)},
		{driver: config.DriverModbusRTUTCP, want: (*modbusrtutcp.Transport)(nil)},
		{driver: config.DriverModbusSerial, want: (*modbusserial.Transport)(nil)},
		{driver: config.DriverRandom, want: (*random.Transport)(nil)},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.driver), func(t *testing.T) {
			t.Parallel()

			got, err := New(config.Plant{Driver: test.driver}, nil)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			gotFacade, ok := got.(*facade)
			if !ok {
				t.Fatalf("New() type = %T, want *facade", got)
			}
			if gotType, wantType := reflect.TypeOf(gotFacade.transport), reflect.TypeOf(test.want); gotType != wantType {
				t.Fatalf("New() transport type = %v, want %v", gotType, wantType)
			}
		})
	}
}

func TestNewRejectsUnknownDriver(t *testing.T) {
	t.Parallel()

	got, err := New(config.Plant{Driver: "other"}, nil)
	if got != nil {
		t.Fatalf("New() = %T, want nil", got)
	}
	if err == nil || err.Error() != "Unknown driver: other" {
		t.Fatalf("New() error = %v", err)
	}
}
