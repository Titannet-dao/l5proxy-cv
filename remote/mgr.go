package remote

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

type MgrConfig struct {
	websocketURL string
	tunnelCount  int
	tunnelCap    int

	protector func(fd uint64)
}

type Mgr struct {
	config  MgrConfig
	index   atomic.Uint64
	tunnels []*WSTunnel

	isActivated bool
}

func NewMgr(config MgrConfig) *Mgr {
	if len(config.websocketURL) == 0 {
		config.websocketURL = "ws://127.0.0.1:8080/ws"
	}

	if config.tunnelCount < 1 {
		config.tunnelCount = 1
	}

	if config.tunnelCap < 1 {
		config.tunnelCap = 100
	}

	mgr := &Mgr{
		config: config,
	}

	return mgr
}

func (mgr *Mgr) Startup() {
	config := &mgr.config

	mgr.tunnels = make([]*WSTunnel, 0, config.tunnelCount)
	for i := 0; i < config.tunnelCount; i++ {
		tnl := newTunnel(config.websocketURL, config.tunnelCap)
		if config.protector != nil {
			tnl.protector = config.protector
		}

		mgr.tunnels = append(mgr.tunnels, tnl)

		tnl.start()
	}

	mgr.isActivated = true

	go mgr.keepalive()
}

func (mgr *Mgr) Shutdown() {
	count := len(mgr.tunnels)
	for i := 0; i < count; i++ {
		tnl := mgr.tunnels[i]
		tnl.stop()
	}

	mgr.isActivated = false
}

func (mgr *Mgr) keepalive() {
	count := len(mgr.tunnels)

	log.Infof("mgr keepalive goroutine start, tunnel count:%d", count)

	for mgr.isActivated {
		time.Sleep(time.Second * 5)

		for i := 0; i < count; i++ {
			tnl := mgr.tunnels[i]
			tnl.keepalive()
		}
	}

	log.Info("mgr keepalive goroutine exit")
}

func (mgr *Mgr) AcceptTCPConn(conn *net.TCPConn) {
	handled := false
	defer func() {
		if !handled {
			conn.Close()
		}
	}()

	// allocate a usable tunnel
	tunnel, err := mgr.allocateWSTunnel()
	if err != nil {
		log.Errorf("WSTunnelMgr.allocateWSTunnel failed: %v", err)
		return
	}

	err = tunnel.acceptTCPConn(conn)
	if err != nil {
		log.Errorf("WSTunnel.acceptTCPConn failed: %v", err)
		return
	}

	handled = true
}

func (mgr *Mgr) AcceptUDPConn(conn *net.UDPConn) {
	handled := false
	defer func() {
		if !handled {
			conn.Close()
		}
	}()

	// allocate a usable tunnel
	tunnel, err := mgr.allocateWSTunnel()
	if err != nil {
		log.Errorf("Mgr.allocateWSTunnel failed: %v", err)
		return
	}

	err = tunnel.acceptUDPConn(conn)
	if err != nil {
		log.Errorf("WSTunnel.acceptUDPConn failed: %v", err)
		return
	}

	handled = true
}

func (mgr *Mgr) nextAllocIndex() uint64 {
	if len(mgr.tunnels) < 1 {
		return 0
	}

	return mgr.index.Add(1) % uint64(len(mgr.tunnels))
}

func (mgr *Mgr) allocateWSTunnel() (*WSTunnel, error) {
	if len(mgr.tunnels) < 1 {
		return nil, fmt.Errorf("tunnels array is empty")
	}

	index := mgr.nextAllocIndex()
	firstIndex := index

	for {
		tnl := mgr.tunnels[index]
		if tnl.isValid() {
			return tnl, nil
		}

		index = mgr.nextAllocIndex()
		if firstIndex == index {
			break
		}
	}

	return nil, fmt.Errorf("failed to find a valid tunnel to accept tcp conn")
}
