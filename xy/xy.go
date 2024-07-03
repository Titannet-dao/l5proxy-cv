package xy

import (
	"fmt"
	"lproxy_tun/local"
	"lproxy_tun/remote"
	"sync"

	log "github.com/sirupsen/logrus"
)

var (
	once    sync.Once
	_Global *XY = nil
)

type XY struct {
	lock sync.Mutex

	local  *local.Mgr
	remote *remote.Mgr
}

func Singleton() *XY {
	once.Do(func() {
		_Global = &XY{}
	})

	return _Global
}

func (xy *XY) Startup() error {
	xy.lock.Lock()
	defer xy.lock.Unlock()
	if xy.local != nil {
		return fmt.Errorf("xy has startup")
	}

	remoteCfg := &remote.MgrConfig{}
	remote := remote.NewMgr(remoteCfg)

	localCfg := &local.LocalConfig{
		TransportHandler: remote,
	}

	local := local.NewMgr(localCfg)

	remote.Startup()
	err := local.Startup()
	if err != nil {
		log.Errorf("local startup failed:%v", err)
	}

	xy.local = local
	xy.remote = remote

	return nil
}

func (xy *XY) Shutdown() error {
	xy.lock.Lock()
	defer xy.lock.Unlock()
	if xy.local != nil {
		return fmt.Errorf("xy has not yet startup")
	}

	err := xy.local.Shutdown()
	if err != nil {
		log.Errorf("local shutdown failed:%v", err)
	}

	xy.remote.Shutdown()

	xy.local = nil
	xy.remote = nil

	return nil
}

func (xy *XY) QueryState() string {
	// TODO: query full state
	return "not implemented yet"
}
