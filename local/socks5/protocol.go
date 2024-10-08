package localsocks5

import (
	"bufio"
	"fmt"
	"io"
	"net"
)

const (
	socks5Version    = uint8(5)
	connectCommand   = uint8(1)
	bindCommand      = uint8(2)
	associateCommand = uint8(3)
	ipv4Address      = uint8(1)
	fqdnAddress      = uint8(3)
	ipv6Address      = uint8(4)

	noAuth       = uint8(0)
	noAcceptable = uint8(255)
)

const (
	successReply uint8 = iota
	serverFailure
	ruleFailure
	networkUnreachable
	hostUnreachable
	connectionRefused
	ttlExpired
	commandNotSupported
	addrTypeNotSupported
)

type noAuthAuthenticator struct{}

func (a noAuthAuthenticator) authenticate(writer io.Writer) error {
	_, err := writer.Write([]byte{socks5Version, noAuth})
	return err
}

func readAuthMethods(r io.Reader) ([]byte, error) {
	header := []byte{0}
	if _, err := r.Read(header); err != nil {
		return nil, err
	}

	numMethods := int(header[0])
	methods := make([]byte, numMethods)
	_, err := io.ReadAtLeast(r, methods, numMethods)
	return methods, err
}

func noAcceptableAuth(conn io.Writer) error {
	conn.Write([]byte{socks5Version, noAcceptable})
	return fmt.Errorf("not support auth method")
}

func authenticate(conn io.Writer, bufConn io.Reader) error {
	// Get the methods
	methods, err := readAuthMethods(bufConn)
	if err != nil {
		return fmt.Errorf("failed to get auth methods: %v", err)
	}

	// Select a usable method
	for _, method := range methods {
		found := method == noAuth
		if found {
			return noAuthAuthenticator{}.authenticate(conn)
		}
	}

	// No usable method found
	return noAcceptableAuth(conn)
}

type addrSpec struct {
	fqdn string
	ip   net.IP
	port int
}

type request struct {
	// protocol version
	version uint8
	// requested command
	command uint8

	// AddrSpec of the desired destination
	destAddr *addrSpec

	conn      net.Conn
	bufreader *bufio.Reader
}

func replySocks5Client(w io.Writer, resp uint8, addr *addrSpec) error {
	// Format the address
	var addrType uint8
	var addrBody []byte
	var addrPort uint16
	switch {
	case addr == nil:
		addrType = ipv4Address
		addrBody = []byte{0, 0, 0, 0}
		addrPort = 0

	case addr.fqdn != "":
		addrType = fqdnAddress
		addrBody = append([]byte{byte(len(addr.fqdn))}, addr.fqdn...)
		addrPort = uint16(addr.port)

	case addr.ip.To4() != nil:
		addrType = ipv4Address
		addrBody = []byte(addr.ip.To4())
		addrPort = uint16(addr.port)

	case addr.ip.To16() != nil:
		addrType = ipv6Address
		addrBody = []byte(addr.ip.To16())
		addrPort = uint16(addr.port)

	default:
		return fmt.Errorf("failed to format address: %v", addr)
	}

	// Format the message
	msg := make([]byte, 6+len(addrBody))
	msg[0] = socks5Version
	msg[1] = resp
	msg[2] = 0 // Reserved
	msg[3] = addrType
	copy(msg[4:], addrBody)
	msg[4+len(addrBody)] = byte(addrPort >> 8)
	msg[4+len(addrBody)+1] = byte(addrPort & 0xff)

	// Send the message
	_, err := w.Write(msg)
	return err
}

func newRequest(bufreader *bufio.Reader, conn net.Conn) (*request, error) {
	// Read the version byte
	header := []byte{0, 0, 0}

	if _, err := io.ReadAtLeast(bufreader, header, 3); err != nil {
		return nil, fmt.Errorf("localsocks5.Mgr failed to get command version: %v", err)
	}

	// Ensure we are compatible
	if header[0] != socks5Version {
		return nil, fmt.Errorf("localsocks5.Mgr unsupported command version: %v", header[0])
	}

	// Read in the destination address
	dest, err := readAddrSpec(bufreader)
	if err != nil {
		return nil, err
	}

	request := &request{
		version:   socks5Version,
		command:   header[1],
		destAddr:  dest,
		conn:      conn,
		bufreader: bufreader,
	}

	return request, nil
}

func readAddrSpec(r io.Reader) (*addrSpec, error) {
	d := &addrSpec{}

	// Get the address type
	addrType := []byte{0}
	if _, err := r.Read(addrType); err != nil {
		return nil, err
	}

	// Handle on a per type basis
	switch addrType[0] {
	case ipv4Address:
		addr := make([]byte, 4)
		if _, err := io.ReadAtLeast(r, addr, len(addr)); err != nil {
			return nil, err
		}
		d.ip = net.IP(addr)
		d.fqdn = string(d.ip.String()) // cast to domain name
	case ipv6Address:
		addr := make([]byte, 16)
		if _, err := io.ReadAtLeast(r, addr, len(addr)); err != nil {
			return nil, err
		}
		d.ip = net.IP(addr)
		d.fqdn = string(d.ip.String()) // cast to domain name
	case fqdnAddress:
		if _, err := r.Read(addrType); err != nil {
			return nil, err
		}
		addrLen := int(addrType[0])
		fqdn := make([]byte, addrLen)
		if _, err := io.ReadAtLeast(r, fqdn, addrLen); err != nil {
			return nil, err
		}
		d.fqdn = string(fqdn)

	default:
		return nil, fmt.Errorf("localsocks5.Mgr unsupport address type:%d", addrType[0])
	}

	// Read the port
	port := []byte{0, 0}
	if _, err := io.ReadAtLeast(r, port, 2); err != nil {
		return nil, err
	}
	d.port = (int(port[0]) << 8) | int(port[1])

	return d, nil
}
