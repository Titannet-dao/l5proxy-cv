package remote

import (
	"encoding/binary"
	"encoding/hex"
	"sync"
	"time"
)

const udpTimeOut = 120 * time.Second

type ChildUstubs map[string]*Ustub

type Cache struct {
	// key=hash(src+dest)
	// ustubs sync.Map
	ustubs map[string]ChildUstubs
	lock   sync.Mutex
}

func newCache() *Cache {
	return &Cache{ustubs: make(map[string]ChildUstubs)}
}

func (c *Cache) add(ustub *Ustub) {
	c.lock.Lock()
	defer c.lock.Unlock()

	parentKey := c.key(ustub.srcAddress())
	childKey := c.key(ustub.destAddress())

	childs := c.ustubs[parentKey]
	if childs == nil {
		childs = make(map[string]*Ustub)
	}

	childs[childKey] = ustub
	c.ustubs[parentKey] = childs
}

func (c *Cache) get(src, dest Address) *Ustub {
	c.lock.Lock()
	defer c.lock.Unlock()

	parentKey := c.key(src)
	childKey := c.key(dest)
	childs := c.ustubs[parentKey]
	if childs == nil {
		return nil
	}
	return childs[childKey]
}

func (c *Cache) getWithSrc(src Address) *Ustub {
	c.lock.Lock()
	defer c.lock.Unlock()

	parentKey := c.key(src)
	childs := c.ustubs[parentKey]
	if childs == nil {
		return nil
	}
	for _, ustub := range childs {
		return ustub
	}

	return nil
}

func (c *Cache) remove(src, dest Address) {
	c.lock.Lock()
	defer c.lock.Unlock()

	parentKey := c.key(src)
	childKey := c.key(dest)

	childs := c.ustubs[parentKey]
	if childs == nil {
		return
	}

	delete(childs, childKey)

	if len(childs) == 0 {
		delete(c.ustubs, parentKey)
		return
	}

	c.ustubs[parentKey] = childs
}

func (c *Cache) keepalive() {
	deleteUstubs := make([]*Ustub, 0)
	for _, childs := range c.ustubs {
		for _, ustub := range childs {
			if time.Since(ustub.lastActvity) > udpTimeOut {
				deleteUstubs = append(deleteUstubs, ustub)
			}
		}
	}

	for _, ustubs := range deleteUstubs {
		c.remove(ustubs.srcAddress(), ustubs.destAddress())
	}
}

func (c *Cache) key(addr Address) string {
	buf := make([]byte, 2+len(addr.ip))

	binary.LittleEndian.PutUint16(buf[0:], addr.port)
	copy(buf[2:], addr.ip)

	return hex.EncodeToString(buf)
}
