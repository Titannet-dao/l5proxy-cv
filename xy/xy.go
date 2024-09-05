package xy

import (
	"fmt"
	"lproxy_tun/config"
	"lproxy_tun/local"
	"lproxy_tun/remote"
	"sync"

	log "github.com/sirupsen/logrus"
)

var (
	once      sync.Once
	singleton *XY = nil
)

type XY struct {
	lock sync.Mutex

	local  *local.Mgr
	remote *remote.Mgr
}

func Singleton() *XY {
	once.Do(func() {
		singleton = &XY{}
	})

	return singleton
}

func (xy *XY) Startup(fd int, mtu uint32, cfg *config.Config) error {
	xy.lock.Lock()
	defer xy.lock.Unlock()

	log.Info("xy.Startup called")
	if xy.local != nil {
		return fmt.Errorf("xy has startup")
	}

	websocketURL := fmt.Sprintf("%s?uuid=%s&endpoint=%s", cfg.Server.URL, cfg.Server.UUID, cfg.Server.Endpiont)
	remoteCfg := &remote.MgrConfig{WebsocketURL: websocketURL, TunnelCount: cfg.Tun.Count, TunnelCap: cfg.Tun.Cap}
	remote := remote.NewMgr(remoteCfg)

	localCfg := &local.LocalConfig{
		TransportHandler: remote,
		FD:               fd,
		MTU:              mtu,
	}

	local := local.NewMgr(localCfg)

	err := remote.Startup()
	if err != nil {
		log.Errorf("remote startup failed:%v", err)
	}

	err = local.Startup()
	if err != nil {
		log.Errorf("local startup failed:%v", err)
	}

	xy.local = local
	xy.remote = remote

	log.Info("xy.Startup completed")
	return nil
}

func (xy *XY) Shutdown() error {
	xy.lock.Lock()
	defer xy.lock.Unlock()

	log.Info("xy.Shutdown called")

	if xy.local == nil {
		return fmt.Errorf("xy has not yet startup")
	}

	err := xy.local.Shutdown()
	if err != nil {
		log.Errorf("local shutdown failed:%v", err)
	}

	err = xy.remote.Shutdown()
	if err != nil {
		log.Errorf("remote shutdown failed:%v", err)
	}

	xy.local = nil
	xy.remote = nil

	log.Info("xy.Shutdown completed")
	return nil
}

func (xy *XY) QueryState() string {
	// TODO: query full state
	return "not implemented yet"
}
