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
	forwardMap sync.Map
	reverseMap sync.Map
}

func newCache() *Cache {
	return &Cache{}
}

func (c *Cache) addForwardUstubs(ustub *Ustub) {
	key := c.keyForward(ustub.srcAddress(), ustub.destAddress())
	c.forwardMap.Store(key, ustub)
}

func (c *Cache) getForwardUstubs(src, dest *net.UDPAddr) *Ustub {
	key := c.keyForward(src, dest)
	v, ok := c.forwardMap.Load(key)
	if ok {
		return v.(*Ustub)
	}
	return nil
}

func (c *Cache) getReverseUstubs(src, dest *net.UDPAddr) *Ustub {
	key := c.keyForward(src, dest)
	v, ok := c.forwardMap.Load(key)
	if ok {
		return v.(*Ustub)
	}

	key = c.keyReverse(dest)
	v, ok = c.reverseMap.Load(key)
	if ok {
		return v.(*Ustub)
	}
	return nil
}

func (c *Cache) addReverseUstubs(ustub *Ustub) {
	key := c.keyReverse(ustub.destAddress())
	c.reverseMap.Store(key, ustub)
}

func (c *Cache) remove(src, dest *net.UDPAddr) {
}

func (c *Cache) keepalive() {
	deleteUstubs := make([]*Ustub, 0)
	c.forwardMap.Range(func(key, value any) bool {
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
		key := c.keyForward(ustub.srcAddress(), ustub.destAddress())
		c.forwardMap.Delete(key)
		log.Infof("delet forwardMap src %s dest %s", ustub.srcAddress().String(), ustub.destAddress().String())
	}

	deleteUstubs = make([]*Ustub, 0)
	c.reverseMap.Range(func(key, value any) bool {
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
		key := c.keyReverse(ustub.destAddress())
		c.reverseMap.Delete(key)
		log.Infof("delet reverseMap  dest %s", ustub.destAddress().String())
	}
}

func (c *Cache) keyForward(src, dest *net.UDPAddr) string {
	buf := make([]byte, 4+len(src.IP)+len(dest.IP))

	binary.LittleEndian.PutUint16(buf[0:], uint16(src.Port))
	copy(buf[2:], src.IP)

	binary.LittleEndian.PutUint16(buf[2+len(src.IP):], uint16(dest.Port))
	copy(buf[4+len(src.IP):], dest.IP)

	return hex.EncodeToString(buf)
}

func (c *Cache) keyReverse(dest *net.UDPAddr) string {
	buf := make([]byte, 2+len(dest.IP))

	binary.LittleEndian.PutUint16(buf[0:], uint16(dest.Port))
	copy(buf[2:], dest.IP)

	return hex.EncodeToString(buf)
}
