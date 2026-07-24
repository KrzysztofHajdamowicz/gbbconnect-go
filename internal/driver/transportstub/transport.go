// Package transportstub provides the temporary base for transports implemented
// by the following Epic D tickets.
package transportstub

import (
	"context"
	"fmt"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/logbuf"
)

// Transport holds construction data until its medium-specific implementation
// replaces the stub in GC-031 through GC-034.
type Transport struct {
	kind config.DriverType
}

// New creates an unimplemented transport placeholder of the requested kind.
func New(
	kind config.DriverType,
	_ config.Plant,
	_ logbuf.Logger,
) *Transport {
	return &Transport{kind: kind}
}

// Connect currently succeeds because concrete transports connect lazily.
func (transport *Transport) Connect(ctx context.Context) error {
	return ctx.Err()
}

// SendRTU reports that the medium-specific implementation is pending.
func (transport *Transport) SendRTU(
	ctx context.Context,
	_ []byte,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%s transport is not implemented", transport.kind)
}

// Close is safe before a concrete transport has opened a resource.
func (transport *Transport) Close() error {
	return nil
}
