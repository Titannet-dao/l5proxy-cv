package remote

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	udpTimeOut            = 120 * time.Second
	udpCacheKeepaliveTime = 30 * time.Second
)

type UdpCache struct {
	// key=hash(src+dst)
	ustubs        sync.Map
	lastKeepalive time.Time
}

func newUdpCache() *UdpCache {
	return &UdpCache{
		lastKeepalive: time.Now(),
	}
}

func (c *UdpCache) add(ustub *UdpStub) {
	key := c.key(ustub.srcAddress(), ustub.dstAddress())
	c.ustubs.Store(key, ustub)
}

func (c *UdpCache) get(src, dst *net.UDPAddr) *UdpStub {
	key := c.key(src, dst)
	v, ok := c.ustubs.Load(key)
	if ok {
		return v.(*UdpStub)
	}
	return nil
}

func (c *UdpCache) keepalive() {
	if time.Since(c.lastKeepalive) < udpCacheKeepaliveTime {
		return
	}

	deleteUstubs := make([]*UdpStub, 0)
	c.ustubs.Range(func(key, value any) bool {
		ustub, ok := value.(*UdpStub)
		if ok {
			if time.Since(ustub.lastActvity) > udpTimeOut {
				deleteUstubs = append(deleteUstubs, ustub)
			}
		}
		return true
	})

	for _, ustub := range deleteUstubs {
		ustub.close()
		key := c.key(ustub.srcAddress(), ustub.dstAddress())
		c.ustubs.Delete(key)
		log.Debugf("delete UDPConn src %s dst %s", ustub.srcAddress().String(), ustub.dstAddress().String())
	}

}

func (c *UdpCache) key(src, dst *net.UDPAddr) string {
	buf := make([]byte, 4+len(src.IP)+len(dst.IP))

	binary.LittleEndian.PutUint16(buf[0:], uint16(src.Port))
	copy(buf[2:], src.IP)

	binary.LittleEndian.PutUint16(buf[2+len(src.IP):], uint16(dst.Port))
	copy(buf[4+len(src.IP):], dst.IP)

	return hex.EncodeToString(buf)
}

func (c *UdpCache) cleanup() {
	c.ustubs.Range(func(key, value any) bool {
		ustub, ok := value.(*UdpStub)
		if ok {
			ustub.close()
		}
		return true
	})
}
