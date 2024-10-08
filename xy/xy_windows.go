package xy

import (
	"fmt"
	"l5proxy_cv/config"
	"l5proxy_cv/meta"
)

func (xy *XY) newTunMode(cfg *config.Config, handler meta.TunTransportHandler) (meta.Local, error) {
	_ = cfg
	_ = handler
	return nil, fmt.Errorf("windows not support tun mode, only works at linux/unix")
}

func setSocketMark(fd, mark int) error {
	_ = fd
	_ = mark
	return fmt.Errorf("windows not support set socket mark")
}
