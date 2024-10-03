package remote

import (
	"net"
	"syscall"
	"testing"
)

func TestResolve(t *testing.T) {
	mark := 0x22 // 34
	protector := func(fd uint64) {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark); err != nil {
			t.Errorf("failed to set mark:%s", err)
		}
	}

	ip, err := resolveHost4("baobei.llwant.com", protector, alibbDNSServer)
	if err != nil {
		t.Errorf("failed resolveHost:%s", err)
	}

	addr := net.TCPAddr{
		IP:   ip,
		Port: 433,
	}

	t.Errorf("DNS reply:%s", addr.String())
}
