package xy

import (
	"fmt"
	"l5proxy_cv/config"
	"l5proxy_cv/meta"
)

func (xy *XY) newTunMode(cfg *config.Config, handler meta.TunTransportHandler,
	bypass meta.Bypass, protector func(fd uint64)) (meta.Local, error) {
	_ = cfg
	_ = handler
	_ = bypass
	_ = protector
	return nil, fmt.Errorf("windows not support tun mode, only works at linux/unix")
}

func setSocketMark(fd, mark int) error {
	_ = fd
	_ = mark
	return fmt.Errorf("windows not support set socket mark")
}
