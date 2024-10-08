package xy

import (
	"fmt"
	"l5proxy_cv/config"
	localhttp "l5proxy_cv/local/http"
	localsocks5 "l5proxy_cv/local/socks5"
	"l5proxy_cv/meta"
	"l5proxy_cv/remote"
	"sync"

	log "github.com/sirupsen/logrus"
)

var (
	once      sync.Once
	singleton *XY = nil
)

type XY struct {
	lock sync.Mutex

	locals []meta.Local
	remote *remote.Mgr
}

func Singleton() *XY {
	once.Do(func() {
		singleton = &XY{}
	})

	return singleton
}

func (xy *XY) Startup(cfg *config.Config) error {
	xy.lock.Lock()
	defer xy.lock.Unlock()

	if xy.locals != nil {
		return fmt.Errorf("xy has startup")
	}

	websocketURL := fmt.Sprintf("%s?uuid=%s&endpoint=%s", cfg.Server.URL, cfg.Server.UUID, cfg.Server.Endpiont)

	var protector func(fd uint64)
	if cfg.Server.Mark > 0 {
		mark := cfg.Server.Mark
		protector = func(fd uint64) {
			setSocketMark(int(fd), mark)
		}
	}

	remoteCfg := &remote.MgrConfig{WebsocketURL: websocketURL, TunnelCount: cfg.Tunnel.Count,
		TunnelCap: cfg.Tunnel.Cap, Protector: protector}
	remote := remote.NewMgr(remoteCfg)

	var locals []meta.Local

	if cfg.TunMode.Enabled {
		l, err := xy.newTunMode(cfg, remote)
		if err != nil {
			log.Errorf("xy.Startup newTunMode failed:%s", err)
		} else {
			locals = append(locals, l)
		}
	}

	if cfg.HTTPMode.Enabled {
		localCfg := &localhttp.LocalConfig{
			TransportHandler: remote,
			Address:          cfg.HTTPMode.Address,
		}

		locals = append(locals, localhttp.NewMgr(localCfg))
	}

	if cfg.Socks5Mode.Enabled {
		localCfg := &localsocks5.LocalConfig{
			TransportHandler: remote,
			Address:          cfg.Socks5Mode.Address,
		}

		locals = append(locals, localsocks5.NewMgr(localCfg))
	}

	err := remote.Startup()
	if err != nil {
		log.Errorf("remote startup failed:%v", err)
	}

	for _, local := range locals {
		err = local.Startup()
		if err != nil {
			log.Errorf("local %s startup failed:%v", local.Name(), err)
		}
	}

	xy.locals = locals
	xy.remote = remote

	log.Info("xy.Startup")
	return nil
}

func (xy *XY) Shutdown() error {
	xy.lock.Lock()
	defer xy.lock.Unlock()

	log.Info("xy.Shutdown called")

	if xy.locals == nil {
		return fmt.Errorf("xy has not yet startup")
	}

	var err error
	for _, local := range xy.locals {
		err = local.Shutdown()
		if err != nil {
			log.Errorf("local %s shutdown failed:%v", local.Name(), err)
		}
	}

	err = xy.remote.Shutdown()
	if err != nil {
		log.Errorf("remote shutdown failed:%v", err)
	}

	xy.locals = nil
	xy.remote = nil

	log.Info("xy.Shutdown completed")
	return nil
}

func (xy *XY) QueryState() string {
	// TODO: query full state
	return "not implemented yet"
}
