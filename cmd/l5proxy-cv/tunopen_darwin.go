package main

import (
	"fmt"
)

func openTun(name string) (int, error) {
	_ = name
	return 0, fmt.Errorf("macos not support tun device")
}
