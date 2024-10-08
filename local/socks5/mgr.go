package localsocks5

import (
	"bufio"
	"fmt"
	"io"
	"l5proxy_cv/meta"
	"net"

	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
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

func readMethods(r io.Reader) ([]byte, error) {
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
	methods, err := readMethods(bufConn)
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

type socks5conn struct {
	*net.TCPConn
}

func (hc socks5conn) ID() *stack.TransportEndpointID {
	return nil
}

type LocalConfig struct {
	Address string

	TransportHandler meta.HTTPSocks5TransportHandler
}

type Mgr struct {
	cfg LocalConfig

	listener *net.TCPListener
}

func NewMgr(cfg *LocalConfig) meta.Local {
	mgr := &Mgr{
		cfg: *cfg,
	}

	return mgr
}

func (mgr *Mgr) Startup() error {
	if mgr.listener != nil {
		return fmt.Errorf("localsocks5.Mgr already startup")
	}

	var err error
	addr, err := net.ResolveTCPAddr("tcp", mgr.cfg.Address)
	if err != nil {
		return fmt.Errorf("localsocks5.Mgr resolve tcp address error:%s", err)
	}

	mgr.listener, err = net.ListenTCP("tcp", addr)
	if err != nil {
		return fmt.Errorf("localsocks5.Mgr ListenTCP error:%s", err)
	}

	go mgr.serveSocks5()

	return nil
}

func (mgr *Mgr) Shutdown() error {
	if mgr.listener == nil {
		return fmt.Errorf("localsocks5.Mgr isn't running")
	}

	err := mgr.listener.Close()
	if err != nil {
		log.Errorf("localsocks5.Mgr shutdown TCP server failed:%s", err)
	}
	return nil
}

func (mgr *Mgr) serveSocks5() {
	for {
		conn, err := mgr.listener.Accept()
		if err != nil {
			log.Errorf("localsocks5.Mgr serveSocks5 error:%s", err)
			return
		}

		go mgr.serveSocks5Conn(conn)
	}
}

func (mgr *Mgr) serveSocks5Conn(conn net.Conn) {
	var handled = false
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("serveSocks5Conn Recovered. Error:%s", r)
		}

		if !handled {
			conn.Close()
		}
	}()

	bufConn := bufio.NewReader(conn)

	// Read the version byte
	version := []byte{0}
	if _, err := bufConn.Read(version); err != nil {
		log.Errorf("localsocks5.Mgr failed to get socks5 version byte: %v", err)
		return
	}

	// Ensure we are compatible
	if version[0] != socks5Version {
		log.Errorf("localsocks5.Mgr Unsupported SOCKS version: %v", version)
		return
	}

	var err = authenticate(conn, bufConn)
	if err != nil {
		log.Errorf("localsocks5.Mgr auth failed: %v", err)
		return
	}

	r1, err := newRequest(bufConn, conn)
	if err != nil {
		log.Errorf("localsocks5.Mgr newRequest error: %v", err)
		return
	}

	err = mgr.handleSocks5Request(r1)
	if err != nil {
		log.Errorf("localsocks5.Mgr handleSocks5Request error: %v", err)
		return
	}

	handled = true
}

func (mgr *Mgr) handleSocks5Request(r *request) error {
	// switch on the command
	switch r.command {
	case connectCommand:
		return mgr.handleSocks5Connect(r)
	case bindCommand:
		return mgr.handleSocks5Bind(r)
	case associateCommand:
		return mgr.handleSocks5Associate(r)
	default:
		if err := replySocks5Client(r.conn, commandNotSupported, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return fmt.Errorf("unsupported command: %v", r.command)
	}
}

func (mgr *Mgr) handleSocks5Connect(req *request) error {
	var extraBytes []byte

	if req.bufreader != nil {
		buffered := req.bufreader.Buffered()
		if buffered > 0 {
			extraBytes = make([]byte, buffered)
			n, err := req.bufreader.Read(extraBytes)
			if err != nil {
				return err
			}

			if n != buffered {
				return fmt.Errorf("handleConnect drain bufreader failed:%s", err)
			}
		}
	}

	thandler := mgr.cfg.TransportHandler
	targetAddress := &meta.HTTPSocksTargetAddress{
		Port:       req.destAddr.port,
		DomainName: req.destAddr.fqdn,
		ExtraBytes: extraBytes,
	}

	tcpconn, ok := req.conn.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("handleConnect socks5 conn isn't tcp conn")
	}

	local := tcpconn.LocalAddr().(*net.TCPAddr)
	bind := addrSpec{ip: local.IP, port: local.Port}
	if err := replySocks5Client(req.conn, successReply, &bind); err != nil {
		return fmt.Errorf("failed to send reply to socks5 client: %v", err)
	}

	socks5conn := socks5conn{
		TCPConn: tcpconn,
	}

	thandler.HandleHttpSocks5TCP(socks5conn, targetAddress)
	return nil
}

func (mgr *Mgr) handleSocks5Bind(req *request) error {
	// TODO: Support bind
	if err := replySocks5Client(req.conn, commandNotSupported, nil); err != nil {
		return fmt.Errorf("failed to send reply: %v", err)
	}

	return fmt.Errorf("unsupport socks5 Bind command")
}

func (mgr *Mgr) handleSocks5Associate(req *request) error {
	// TODO: Support associate
	if err := replySocks5Client(req.conn, commandNotSupported, nil); err != nil {
		return fmt.Errorf("failed to send reply: %v", err)
	}

	return fmt.Errorf("unsupport socks5 Associate command")
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
