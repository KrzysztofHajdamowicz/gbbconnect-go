package driver

import (
	"context"
	"errors"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
)

const (
	readDelay  = 100 * time.Millisecond
	writeDelay = 3 * time.Second
)

// Transport sends and receives complete Modbus RTU frames over one medium.
type Transport interface {
	Connect(ctx context.Context) error
	SendRTU(ctx context.Context, rtu []byte) ([]byte, error)
	Close() error
}

// Driver adds Modbus helpers, transaction serialization, and local-operation
// timing to a Transport.
type Driver interface {
	// Connect eagerly establishes the underlying transport connection.
	Connect(ctx context.Context) error
	// SendDataToDevice is the raw cloud path. It neither delays nor interprets
	// the returned RTU frame.
	SendDataToDevice(ctx context.Context, rtu []byte) ([]byte, error)
	ReadHoldingRegisters(
		ctx context.Context,
		unit byte,
		start uint16,
		count uint16,
	) ([]byte, error)
	WriteMultipleRegisters(
		ctx context.Context,
		unit byte,
		start uint16,
		values []byte,
	) error
	Close() error
}

// ErrTooManyRegistersToRead matches the original local helper error.
//
//nolint:staticcheck // The compatibility contract requires this exact text.
var ErrTooManyRegistersToRead = errors.New("Too much registers to read!")

// ErrTooManyRegistersToWrite matches the original local helper error.
//
//nolint:staticcheck // The compatibility contract requires this exact text.
var ErrTooManyRegistersToWrite = errors.New("Too much registers to write!")

type facade struct {
	transport Transport
	executor  *executor
}

// Wrap adds Driver behavior to an already constructed Transport. It is useful
// for tests and for transports supplied by embedding applications.
func Wrap(transport Transport) Driver {
	return newFacade(transport, nil)
}

func newFacade(transport Transport, clock Clock) *facade {
	return &facade{
		transport: transport,
		executor:  newExecutor(clock),
	}
}

func (driver *facade) Connect(ctx context.Context) error {
	_, err := driver.executor.execute(ctx, 0, false, func() ([]byte, error) {
		return nil, driver.transport.Connect(ctx)
	})
	return err
}

func (driver *facade) SendDataToDevice(
	ctx context.Context,
	rtu []byte,
) ([]byte, error) {
	return driver.executor.execute(ctx, 0, false, func() ([]byte, error) {
		return driver.transport.SendRTU(ctx, rtu)
	})
}

func (driver *facade) ReadHoldingRegisters(
	ctx context.Context,
	unit byte,
	start uint16,
	count uint16,
) ([]byte, error) {
	request := modbus.BuildReadHoldingRegisters(unit, start, count)
	if request == nil {
		return nil, ErrTooManyRegistersToRead
	}

	return driver.executeLocal(ctx, readDelay, request)
}

func (driver *facade) WriteMultipleRegisters(
	ctx context.Context,
	unit byte,
	start uint16,
	values []byte,
) error {
	request := modbus.BuildWriteMultipleRegisters(unit, start, values)
	if request == nil {
		return ErrTooManyRegistersToWrite
	}

	_, err := driver.executeLocal(ctx, writeDelay, request)
	return err
}

func (driver *facade) executeLocal(
	ctx context.Context,
	minimumDelay time.Duration,
	request []byte,
) ([]byte, error) {
	return driver.executor.execute(ctx, minimumDelay, true, func() ([]byte, error) {
		response, err := driver.transport.SendRTU(ctx, request)
		if err != nil {
			return nil, err
		}
		_, data, err := modbus.ParseResponse(response)
		return data, err
	})
}

func (driver *facade) Close() error {
	_, err := driver.executor.execute(
		context.Background(),
		0,
		false,
		func() ([]byte, error) {
			return nil, driver.transport.Close()
		},
	)
	return err
}

var _ Driver = (*facade)(nil)
