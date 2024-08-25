package local

import (
	"fmt"
	"lproxy_tun/meta"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

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
