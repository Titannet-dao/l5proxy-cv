package remote

import (
	"fmt"
	"lproxy_tun/meta"
	"net"
	"time"

	log "github.com/sirupsen/logrus"
)

type Ustub struct {
	tnl         *WSTunnel
	conn        meta.UDPConn
	lastActvity time.Time
}

func newUstub(tnl *WSTunnel, conn meta.UDPConn) *Ustub {
	ustub := &Ustub{tnl: tnl, conn: conn}
	return ustub
}

func (u *Ustub) srcAddress() *net.UDPAddr {
	conn := u.conn
	address := &net.UDPAddr{Port: int(conn.ID().RemotePort), IP: conn.ID().RemoteAddress.AsSlice()}
	return address
}

func (u *Ustub) destAddress() *net.UDPAddr {
	conn := u.conn

	address := &net.UDPAddr{Port: int(conn.ID().LocalPort), IP: conn.ID().LocalAddress.AsSlice()}
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
