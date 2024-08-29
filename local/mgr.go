package local

import (
	"fmt"
	"lproxy_tun/meta"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	log "github.com/sirupsen/logrus"
)

type LocalConfig struct {
	FD               int
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

func (mgr *Mgr) Startup() error {
	log.Info("local.Mgr Startup called")

	if mgr.stack != nil {
		return fmt.Errorf("local.Mgr already startup")
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

	log.Info("local.Mgr Startup completed")
	return nil
}

func (mgr *Mgr) Shutdown() error {
	log.Info("local.Mgr shutdown called")

	if mgr.stack == nil {
		return fmt.Errorf("local.Mgr isn't running")
	}

	// LinkEndPoint must close first
	_ = unix.Close(mgr.cfg.FD)

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
	var (
		wq waiter.Queue
	)

	transport := mgr.stack.TransportProtocolInstance(tcp.ProtocolNumber)
	ep, err := transport.NewEndpoint(ipv4.ProtocolNumber, &wq)
	if err != nil {
		return nil, fmt.Errorf("local.Mgr NewTCP4 NewEndpoint failed:%s", err)
	}

	// TODO: bind to non-local address/port

	// TODO: connect to remote address/port

	conn := &tcpConn{
		TCPConn: gonet.NewTCPConn(&wq, ep),
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
