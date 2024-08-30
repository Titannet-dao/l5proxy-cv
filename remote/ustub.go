package remote

import (
	"fmt"
	"lproxy_tun/meta"
	"net"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

type Address struct {
	ip   []byte
	port uint16
}

func (a Address) ipString() string {
	ip := ""
	for _, b := range a.ip {
		ip = ip + fmt.Sprintf("%d", b) + "."
	}

	return strings.TrimSuffix(ip, ".")
}

type Ustub struct {
	tnl         *WSTunnel
	conn        meta.UDPConn
	lastActvity time.Time
}

func newUstub(tnl *WSTunnel, conn meta.UDPConn) *Ustub {
	ustub := &Ustub{tnl: tnl, conn: conn}
	return ustub
}

func (u *Ustub) srcAddress() Address {
	conn := u.conn

	address := Address{port: conn.ID().RemotePort}
	if conn.ID().RemoteAddress.Len() > 4 {
		src := conn.ID().RemoteAddress.As16()
		address.ip = src[:]
	} else {
		src := conn.ID().RemoteAddress.As4()
		address.ip = src[:]
	}

	return address
}

func (u *Ustub) destAddress() Address {
	conn := u.conn

	address := Address{port: conn.ID().LocalPort}
	if conn.ID().LocalAddress.Len() > 4 {
		dest := conn.ID().LocalAddress.As16()
		address.ip = dest[:]
	} else {
		dest := conn.ID().LocalAddress.As4()
		address.ip = dest[:]
	}

	return address
}

func (u *Ustub) writeTo(data []byte, addr net.Addr) error {
	conn := u.conn
	if conn == nil {
		return fmt.Errorf("Write udp conn == nil")
	}

	u.lastActvity = time.Now()

	wrote := 0
	l := len(data)
	for {
		n, err := conn.WriteTo(data[wrote:], addr)
		if err != nil {
			return err
		}

		wrote = wrote + n
		if wrote == l {
			break
		}
	}

	return nil
}

func (u *Ustub) onUDPMessage(data []byte) {
	u.lastActvity = time.Now()
	u.tnl.onClientUDPData(data, u.srcAddress(), u.destAddress())
}

func (u *Ustub) proxy() {
	conn := u.conn
	if conn == nil {
		log.Error("conn == nil")
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Error("UDP read error %s", err.Error())
			break
		}

		if n == 0 {
			log.Error("UDP read n==0")
			break
		}

		u.onUDPMessage(buf[:n])

	}
}

func (u *Ustub) close() {
	u.conn.Close()
}
