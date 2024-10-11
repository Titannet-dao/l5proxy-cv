package xy

import (
	"l5proxy_cv/config"
	localtun "l5proxy_cv/local/tun"
	"l5proxy_cv/meta"
	"os"
	"syscall"

	log "github.com/sirupsen/logrus"
)

func (xy *XY) newTunMode(cfg *config.Config, handler meta.TunTransportHandler,
	bypass meta.Bypass, protector func(fd uint64)) (meta.Local, error) {

	localCfg := &localtun.LocalConfig{
		TransportHandler: handler,
		FD:               cfg.TunMode.FD,
		MTU:              cfg.TunMode.MTU,
		UseBypass:        cfg.TunMode.Enabled && bypass != nil,
		BypassHandler:    bypass,
		Device:           cfg.TunMode.Device,
		Protector:        protector,
	}

	return localtun.NewMgr(localCfg), nil
}

func setSocketMark(fd, mark int) error {
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_MARK, mark); err != nil {
		log.Errorf("failed to set socket mark:%s", err)
		return os.NewSyscallError("failed to set mark", err)
	}
	return nil
}
