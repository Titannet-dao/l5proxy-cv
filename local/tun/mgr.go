package localtun

import (
	"context"
	"fmt"
	"l5proxy_cv/meta"
	"net"
	"net/netip"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	mkdns "github.com/miekg/dns"
	log "github.com/sirupsen/logrus"
)

type LocalConfig struct {
	FD     int
	MTU    uint32
	Device string

	TransportHandler meta.TunTransportHandler

	TCPModerateReceiveBuffer bool
	TCPSendBufferSize        int
	TCPReceiveBufferSize     int

	UseBypass     bool
	BypassHandler meta.Bypass

	NSHint string

	Protector func(fd uint64)

	AliDNS string
}

type Mgr struct {
	cfg LocalConfig

	tun   *TUN
	stack *stack.Stack

	nsAddrHint netip.AddrPort

	ipdomainRepo *domainIPRepo
}

func NewMgr(cfg *LocalConfig) meta.Local {
	mgr := &Mgr{
		cfg:          *cfg,
		stack:        nil,
		ipdomainRepo: newDomainIPRepo(),
	}

	addrstr := cfg.NSHint
	if len(addrstr) == 0 {
		addrstr = "8.8.8.8:53"
	}

	udpAddr, _ := netip.ParseAddrPort(addrstr)
	mgr.nsAddrHint = udpAddr
	return mgr
}

func (mgr *Mgr) Name() string {
	return "tunmode"
}

func (mgr *Mgr) Startup() error {
	if mgr.stack != nil {
		return fmt.Errorf("localtun.Mgr already startup")
	}
	tun, err := newTUN(mgr.cfg.FD, mgr.cfg.MTU)
	if err != nil {
		return err
	}

	mgr.tun = tun

	stack, err := mgr.createStack()
	if err != nil {
		return err
	}

	mgr.stack = stack

	mgr.cfg.TransportHandler.OnStackReady(mgr)

	log.Infof("Tun server startup, tun device:%s", mgr.cfg.Device)
	return nil
}

func (mgr *Mgr) Shutdown() error {
	log.Info("localtun.Mgr shutdown called")

	if mgr.stack == nil {
		return fmt.Errorf("localtun.Mgr isn't running")
	}

	// LinkEndPoint must close first
	tunclose(mgr.cfg.FD)

	// lingh: Wait() will hang up forever, it seems like a bug in gVisor's stack
	// wait all goroutines to stop
	// mgr.tun.Wait()

	// close all transport endpoints
	mgr.stack.Close()
	// wait all goroutines to stop
	log.Info("localtun.Mgr waiting stack goroutines to stop")
	mgr.stack.Wait()

	log.Info("localtun.Mgr shutdown completed")
	return nil
}

func (mgr *Mgr) HandleTCP(conn meta.TCPConn) {
	handled := false
	defer func() {
		if !handled {
			conn.Close()
		}
	}()

	cfg := mgr.cfg

	if cfg.UseBypass && cfg.BypassHandler != nil && mgr.isBypassIPv4(conn.ID().LocalAddress) {
		go cfg.BypassHandler.HandleHttpSocks5TCP(conn, nil)
	} else {
		go cfg.TransportHandler.HandleTCP(conn)
	}

	handled = true
}

func (mgr *Mgr) HandleUDP(conn meta.UDPConn, extra []byte) {
	_ = extra
	handled := false
	defer func() {
		if !handled {
			conn.Close()
		}
	}()

	if mgr.cfg.UseBypass && mgr.cfg.BypassHandler != nil && mgr.isMyHint(conn.ID()) {
		// handle our DNS query
		go mgr.handleDNSQuery(conn)
	} else {
		remoteHandler := mgr.cfg.TransportHandler
		go remoteHandler.HandleUDP(conn, nil)
	}

	handled = true
}

func (mgr *Mgr) catchDNSResult(data []byte) {
	resp := new(mkdns.Msg)
	err := resp.Unpack(data)
	if err != nil {
		log.Errorf("localtun.Mgr hookDNSResult error:%s", err)
		return
	}

	if len(resp.Question) == 0 {
		log.Error("localtun.Mgr dns reply question field is empty")
		return
	}

	domain := resp.Question[0]
	domainName := domain.Name
	domainName = domain.Name[0 : len(domainName)-1] // remove last '.'
	bypass := mgr.cfg.BypassHandler.BypassAbleDomain(domainName)
	if !bypass {
		return
	}

	ips := make([]net.IP, 0, len(resp.Answer))
	for _, answer := range resp.Answer {
		t, ok := answer.(*mkdns.A)
		if ok {
			ips = append(ips, t.A)
		}
	}

	if len(ips) > 0 {
		mgr.ipdomainRepo.updateDomain(domainName, ips)
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func (mgr *Mgr) isMyHint(id *stack.TransportEndpointID) bool {

	ip := id.LocalAddress
	port := id.LocalPort

	ip2 := ip.AsSlice()
	if mgr.nsAddrHint.Port() == port && equalBytes([]byte(mgr.nsAddrHint.Addr().AsSlice()), ip2) {
		return true
	}

	return false
}

func (mgr *Mgr) OnStackReady(lgv meta.LocalGivsorNetwork) {
	remoteHandler := mgr.cfg.TransportHandler
	remoteHandler.OnStackReady(lgv)
}

func (mgr *Mgr) isBypassIPv4(address tcpip.Address) bool {
	return mgr.ipdomainRepo.lookup(address.AsSlice())
}

func (mgr *Mgr) createStack() (*stack.Stack, error) {
	log.Info("localtun.Mgr createStack: new gVisor network stack")

	var opts []Option
	if mgr.cfg.TCPModerateReceiveBuffer {
		opts = append(opts, WithTCPModerateReceiveBuffer(true))
	}

	if mgr.cfg.TCPSendBufferSize != 0 {
		opts = append(opts, WithTCPSendBufferSize(mgr.cfg.TCPSendBufferSize))
	}

	if mgr.cfg.TCPReceiveBufferSize != 0 {
		opts = append(opts, WithTCPReceiveBufferSize(mgr.cfg.TCPReceiveBufferSize))
	}

	stackCfg := &StackConfig{
		LinkEndpoint:     mgr.tun.LinkEndpoint,
		TransportHandler: mgr,
		Options:          opts,
	}

	return createStack(stackCfg)
}

func (mgr *Mgr) NewTCP4(id *stack.TransportEndpointID) (meta.TCPConn, error) {
	localAddr := tcpip.FullAddress{
		Addr: id.LocalAddress,
		Port: id.LocalPort,
	}

	remoteAddr := tcpip.FullAddress{
		Addr: id.RemoteAddress,
		Port: id.RemotePort,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tconn, terr := gonet.DialTCPWithBind(ctx, mgr.stack, localAddr, remoteAddr, ipv4.ProtocolNumber)
	if terr != nil {
		// ep.Close()
		return nil, fmt.Errorf("localtun.Mgr NewTCP4: DialTCP failed:%s", terr.Error())
	}

	conn := &tcpConn{
		TCPConn: tconn, //gonet.NewTCPConn(&wq, ep),
		id:      *id,
	}

	return conn, nil
}

func (mgr *Mgr) NewUDP4(id *stack.TransportEndpointID) (meta.UDPConn, error) {
	var (
		wq waiter.Queue
	)

	transport := mgr.stack.TransportProtocolInstance(udp.ProtocolNumber)
	ep, err := transport.NewEndpoint(ipv4.ProtocolNumber, &wq)
	if err != nil {
		return nil, fmt.Errorf("localtun.Mgr NewUDP4: NewEndpoint failed:%s", err)
	}

	fullAddr := tcpip.FullAddress{
		Addr: id.LocalAddress,
		Port: id.LocalPort,
	}

	// reuse address
	ep.SocketOptions().SetReuseAddress(true)

	// bind to a 'non-local' address/port
	err = ep.Bind(fullAddr)
	if err != nil {
		ep.Close()
		return nil, fmt.Errorf("localtun.Mgr NewUDP4: Bind UDP failed:%s", err)
	}

	// lingh: UDP does not connect to specific peer, thus allow it send to many peers
	// err = ep.Connect(fullAddr2)

	conn := &udpConn{
		UDPConn: gonet.NewUDPConn(&wq, ep),
		id:      *id,
	}

	return conn, nil
}
