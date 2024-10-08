package meta

import (
	"net"

	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// TCPConn implements the net.Conn interface.
type TCPConn interface {
	net.Conn

	// ID returns the transport endpoint id of TCPConn.
	ID() *stack.TransportEndpointID
	CloseWrite() error
	CloseRead() error
}

// UDPConn implements net.Conn and net.PacketConn.
type UDPConn interface {
	net.Conn
	net.PacketConn

	// ID returns the transport endpoint id of UDPConn.
	ID() *stack.TransportEndpointID
}

// TunTransportHandler is a TCP/UDP connection handler that implements
// HandleTCP and HandleUDP methods.
type TunTransportHandler interface {
	HandleTCP(TCPConn)
	HandleUDP(UDPConn)

	OnStackReady(LocalGivsorNetwork)
}

// LocalGivsorNetwork is for creating connections
type LocalGivsorNetwork interface {
	NewTCP4(id *stack.TransportEndpointID) (TCPConn, error)
	NewUDP4(id *stack.TransportEndpointID) (UDPConn, error)
}

type HTTPSocksTargetAddress struct {
	DomainName string
	Port       int

	ExtraBytes []byte
}

type HTTPSocks5TransportHandler interface {
	HandleHttpSocks5TCP(TCPConn, *HTTPSocksTargetAddress)
}

type Local interface {
	Startup() error
	Shutdown() error
	Name() string
}
