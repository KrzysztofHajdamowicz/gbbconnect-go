package modbusrtutcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/modbus"
)

func TestTransportReassemblesFragmentedRead(t *testing.T) {
	t.Parallel()

	listener := listenTCP(t)
	serverErrors := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		defer func() {
			_ = connection.Close()
		}()

		request := modbus.BuildReadHoldingRegisters(1, 0, 2)
		if err := expectRequest(connection, request); err != nil {
			serverErrors <- err
			return
		}
		response := modbus.AppendCRC([]byte{1, 3, 4, 0, 1, 0, 2})
		if _, err := connection.Write(response[:2]); err != nil {
			serverErrors <- err
			return
		}
		if _, err := connection.Write(response[2:]); err != nil {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	request := modbus.BuildReadHoldingRegisters(1, 0, 2)
	got, err := transport.SendRTU(testContext(t), request)
	if err != nil {
		t.Fatalf("SendRTU() error = %v", err)
	}
	want := modbus.AppendCRC([]byte{1, 3, 4, 0, 1, 0, 2})
	if !bytes.Equal(got, want) {
		t.Fatalf("SendRTU() = %X, want %X", got, want)
	}
	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
}

func TestTransportBuffersCoalescedWriteResponse(t *testing.T) {
	t.Parallel()

	listener := listenTCP(t)
	serverErrors := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		defer func() {
			_ = connection.Close()
		}()

		firstRequest := modbus.BuildReadHoldingRegisters(1, 0, 1)
		if err := expectRequest(connection, firstRequest); err != nil {
			serverErrors <- err
			return
		}
		firstResponse := modbus.AppendCRC([]byte{1, 3, 2, 0, 1})
		secondResponse := modbus.AppendCRC([]byte{1, 16, 0, 16, 0, 1})
		coalesced := append(bytes.Clone(firstResponse), secondResponse...)
		if _, err := connection.Write(coalesced); err != nil {
			serverErrors <- err
			return
		}

		secondRequest := modbus.BuildWriteMultipleRegisters(
			1,
			0x0010,
			[]byte{0x12, 0x34},
		)
		if err := expectRequest(connection, secondRequest); err != nil {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	first, err := transport.SendRTU(
		testContext(t),
		modbus.BuildReadHoldingRegisters(1, 0, 1),
	)
	if err != nil {
		t.Fatalf("first SendRTU() error = %v", err)
	}
	if want := modbus.AppendCRC([]byte{1, 3, 2, 0, 1}); !bytes.Equal(first, want) {
		t.Fatalf("first SendRTU() = %X, want %X", first, want)
	}

	second, err := transport.SendRTU(
		testContext(t),
		modbus.BuildWriteMultipleRegisters(1, 0x0010, []byte{0x12, 0x34}),
	)
	if err != nil {
		t.Fatalf("second SendRTU() error = %v", err)
	}
	if want := modbus.AppendCRC([]byte{1, 16, 0, 16, 0, 1}); !bytes.Equal(second, want) {
		t.Fatalf("second SendRTU() = %X, want %X", second, want)
	}
	if len(transport.pending) != 0 {
		t.Fatalf("pending bytes after two responses = %X", transport.pending)
	}
	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
}

func TestTransportReturnsCRCErrorWithoutRetry(t *testing.T) {
	t.Parallel()

	listener := listenTCP(t)
	serverErrors := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		defer func() {
			_ = connection.Close()
		}()
		request := modbus.BuildReadHoldingRegisters(1, 0, 1)
		if err := expectRequest(connection, request); err != nil {
			serverErrors <- err
			return
		}
		response := modbus.AppendCRC([]byte{1, 3, 2, 0, 1})
		response[len(response)-1] ^= 0xFF
		if _, err := connection.Write(response); err != nil {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	_, err := transport.SendRTU(
		testContext(t),
		modbus.BuildReadHoldingRegisters(1, 0, 1),
	)
	if !errors.Is(err, ErrWrongCRC) {
		t.Fatalf("SendRTU() error = %v, want ErrWrongCRC", err)
	}
	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
}

func TestTransportReconnectsAfterDrop(t *testing.T) {
	t.Parallel()

	listener := listenTCP(t)
	serverErrors := make(chan error, 1)
	go func() {
		first, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		request := modbus.BuildReadHoldingRegisters(1, 0, 1)
		if err := expectRequest(first, request); err != nil {
			serverErrors <- err
			return
		}
		if err := first.Close(); err != nil {
			serverErrors <- err
			return
		}

		second, err := listener.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		defer func() {
			_ = second.Close()
		}()
		if err := expectRequest(second, request); err != nil {
			serverErrors <- err
			return
		}
		response := modbus.AppendCRC([]byte{1, 3, 2, 0, 1})
		if _, err := second.Write(response); err != nil {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	transport := newLoopbackTransport(t, listener)
	transport.retryDelay = 0
	if _, err := transport.SendRTU(
		testContext(t),
		modbus.BuildReadHoldingRegisters(1, 0, 1),
	); err != nil {
		t.Fatalf("SendRTU() error = %v", err)
	}
	if err := <-serverErrors; err != nil {
		t.Fatalf("loopback server error = %v", err)
	}
}

func listenTCP(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return listener
}

func newLoopbackTransport(t *testing.T, listener *net.TCPListener) *Transport {
	t.Helper()
	transport := New(
		config.Plant{
			Address: "127.0.0.1",
			Port:    listener.Addr().(*net.TCPAddr).Port,
		},
		nil,
	)
	transport.timeout = time.Second
	transport.retryDelay = 5 * time.Millisecond
	t.Cleanup(func() {
		_ = transport.Close()
	})
	return transport
}

func expectRequest(connection net.Conn, want []byte) error {
	got := make([]byte, len(want))
	if _, err := io.ReadFull(connection, got); err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("gateway received unexpected request")
	}
	return nil
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}
