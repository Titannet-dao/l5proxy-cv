package local

// base on:
// https://github.com/xjasonlyu/tun2socks/blob/main/core/device/tun/tun_netstack.go
import (
	"fmt"

	fdbased "gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	stack "gvisor.dev/gvisor/pkg/tcpip/stack"
)

type TUN struct {
	stack.LinkEndpoint

	fd  int
	mtu uint32
}

func newTUN(fd int, mtu uint32) (*TUN, error) {
	tun := &TUN{
		fd:  fd,
		mtu: mtu,
	}

	ep, err := fdbased.New(&fdbased.Options{
		FDs: []int{fd},
		MTU: tun.mtu,
		// TUN only, ignore ethernet header.
		EthernetHeader: false,
		// SYS_READV support only for TUN fd.
		PacketDispatchMode: fdbased.Readv,
		// TAP/TUN fd's are not sockets and using the WritePackets calls results
		// in errors as it always defaults to using SendMMsg which is not supported
		// for tap/tun device fds.
		//
		// This CL changes WritePackets to gracefully degrade to using writev instead
		// of sendmmsg if the underlying fd is not a socket.
		//
		// Fixed: https://github.com/google/gvisor/commit/f33d034fecd7723a1e560ccc62aeeba328454fd0
		MaxSyscallHeaderBytes: 0x00,
	})

	if err != nil {
		return nil, fmt.Errorf("create endpoint: %w", err)
	}

	tun.LinkEndpoint = ep

	return tun, nil
}
