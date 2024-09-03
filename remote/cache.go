package remote

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"sync"
	"time"

	"gvisor.dev/gvisor/pkg/log"
)

const udpTimeOut = 120 * time.Second

type Cache struct {
	// key=hash(src+dest)
	ustubs sync.Map
}

func newCache() *Cache {
	return &Cache{}
}

func (c *Cache) add(ustub *Ustub) {
	key := c.key(ustub.srcAddress(), ustub.destAddress())
	c.ustubs.Store(key, ustub)
}

func (c *Cache) get(src, dest *net.UDPAddr) *Ustub {
	key := c.key(src, dest)
	v, ok := c.ustubs.Load(key)
	if ok {
		return v.(*Ustub)
	}
	return nil
}

func (c *Cache) keepalive() {
	deleteUstubs := make([]*Ustub, 0)
	c.ustubs.Range(func(key, value any) bool {
		ustub, ok := value.(*Ustub)
		if ok {
			if time.Since(ustub.lastActvity) > udpTimeOut {
				deleteUstubs = append(deleteUstubs, ustub)
			}
		}
		return true
	})

	for _, ustub := range deleteUstubs {
		ustub.close()
		key := c.key(ustub.srcAddress(), ustub.destAddress())
		c.ustubs.Delete(key)
		log.Infof("delete UDPConn src %s dest %s", ustub.srcAddress().String(), ustub.destAddress().String())
	}

}

func (c *Cache) key(src, dest *net.UDPAddr) string {
	buf := make([]byte, 4+len(src.IP)+len(dest.IP))

	binary.LittleEndian.PutUint16(buf[0:], uint16(src.Port))
	copy(buf[2:], src.IP)

	binary.LittleEndian.PutUint16(buf[2+len(src.IP):], uint16(dest.Port))
	copy(buf[4+len(src.IP):], dest.IP)

	return hex.EncodeToString(buf)
}

func (c *Cache) cleanup() {
	c.ustubs.Range(func(key, value any) bool {
		ustub, ok := value.(*Ustub)
		if ok {
			ustub.close()
		}
		return true
	})
}
