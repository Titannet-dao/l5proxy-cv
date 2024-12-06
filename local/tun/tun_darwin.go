package localtun

import "fmt"

func newTUN(fd int, mtu uint32) (*TUN, error) {
	_ = fd
	_ = mtu
	return nil, fmt.Errorf("macos not support Tun")
}

func tunclose(fd int) {
	_ = fd
}
