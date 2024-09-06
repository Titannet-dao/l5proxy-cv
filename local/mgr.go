package local

import (
	"context"
	"errors"
	"fmt"
	"lproxy_tun/meta"
	"net"
	"time"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip/link/tun"
)

type LocalConfig struct {
	TunName          string
	MTU              uint32
	TransportHandler meta.TransportHandler

	TCPModerateReceiveBuffer bool
	TCPSendBufferSize        int
	TCPReceiveBufferSize     int
}

type Mgr struct {
	cfg LocalConfig

	tun   *TUN
	stack *stack.Stack
}

func NewMgr(cfg *LocalConfig) *Mgr {
	mgr := &Mgr{
		cfg:   *cfg,
		stack: nil,
	}

	return mgr
}

func openTun(name string) (int, error) {
	if len(name) >= unix.IFNAMSIZ {
		return -1, fmt.Errorf("interface name too long: %s", name)
	}

	fd, err := tun.Open(name)
	if err != nil {
		return -1, fmt.Errorf("create tun: %w", err)
	}

	return fd, nil
}

func (mgr *Mgr) Startup() error {
	log.Info("local.Mgr Startup called")

	if mgr.stack != nil {
		return fmt.Errorf("local.Mgr already startup")
	}

	fd, err := openTun(mgr.cfg.TunName)
	if err != nil {
		return err
	}

	tun, err := newTUN(fd, mgr.cfg.MTU)
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

	log.Info("local.Mgr Startup completed")
	return nil
}

func (mgr *Mgr) Shutdown() error {
	log.Info("local.Mgr shutdown called")

	if mgr.stack == nil {
		return fmt.Errorf("local.Mgr isn't running")
	}

	// LinkEndPoint must close first
	_ = unix.Close(mgr.tun.fd)

	// lingh: Wait() will hang up forever, it seems like a bug in gVisor's stack
	// wait all goroutines to stop
	// mgr.tun.Wait()

	// close all transport endpoints
	mgr.stack.Close()
	// wait all goroutines to stop
	log.Info("local.Mgr waiting stack goroutines to stop")
	mgr.stack.Wait()

	log.Info("local.Mgr shutdown completed")
	return nil
}

func (mgr *Mgr) createStack() (*stack.Stack, error) {
	log.Info("local.Mgr createStack: new gVisor network stack")

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
		TransportHandler: mgr.cfg.TransportHandler,
		Options:          opts,
	}

	return createStack(stackCfg)
}

func (mgr *Mgr) NewTCP4(id *stack.TransportEndpointID) (meta.TCPConn, error) {
	localIP, err := mgr.GetTunIP()
	if err != nil {
		return nil, err
	}

	log.Infof("bind local ip %#v", localIP)

	localAddr := tcpip.FullAddress{
		Addr: tcpip.AddrFromSlice(localIP),
		Port: 0,
	}

	remoteAddr := tcpip.FullAddress{
		Addr: id.RemoteAddress,
		Port: id.RemotePort,
	}

	// ep.Connect(remoteAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tconn, terr := gonet.DialTCPWithBind(ctx, mgr.stack, localAddr, remoteAddr, ipv4.ProtocolNumber)
	if terr != nil {
		// ep.Close()
		return nil, fmt.Errorf("local.Mgr NewTCP4: DialTCP failed:%s", terr.Error())
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
		return nil, fmt.Errorf("local.Mgr NewUDP4: NewEndpoint failed:%s", err)
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
		return nil, fmt.Errorf("local.Mgr NewUDP4: Bind UDP failed:%s", err)
	}

	// lingh: UDP does not connect to specific peer, thus allow it send to many peers
	// err = ep.Connect(fullAddr2)

	conn := &udpConn{
		UDPConn: gonet.NewUDPConn(&wq, ep),
		id:      *id,
	}

	return conn, nil
}

func (mgr *Mgr) GetTunIP() (net.IP, error) {
	ief, err := net.InterfaceByName(mgr.cfg.TunName)
	if err != nil {
		return nil, err
	}

	addrs, err := ief.Addrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs { // get ipv4 address
		if addr.(*net.IPNet).IP.To4() != nil {
			ipnet := addr.(*net.IPNet)
			mask := ipnet.Mask
			ip := ipnet.IP.Mask(mask)
			ip[len(ip)-1] += 1

			return ip, nil
		}
	}
	return nil, errors.New(fmt.Sprintf("interface %s don't have an ipv4 address\n", mgr.cfg.TunName))
}
