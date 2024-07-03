package local

import (
	"fmt"
	"lproxy_tun/meta"

	"gvisor.dev/gvisor/pkg/tcpip/stack"
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
	if mgr.stack != nil {
		return fmt.Errorf("local mgr already startup")
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

	return nil
}

func (mgr *Mgr) Shutdown() error {
	if mgr.stack == nil {
		return fmt.Errorf("local mgr isn't running")
	}

	return nil
}

func (mgr *Mgr) createStack() (*stack.Stack, error) {
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
