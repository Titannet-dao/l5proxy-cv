package localtun

import (
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/gaissmai/bart"
)

type domainNode struct {
	name       string
	lastUpdate time.Time
	ips        []net.IP
}

type domainIPRepo struct {
	domainsLock sync.Mutex
	domains     map[string]*domainNode

	treeLock sync.Mutex
	tree     bart.Table[struct{}]
}

func newDomainIPRepo() *domainIPRepo {
	r := &domainIPRepo{
		domains: make(map[string]*domainNode),
	}

	r.setupLocalIPs()

	return r
}

func (r *domainIPRepo) setupLocalIPs() {
	var bits = 32
	addr, _ := netip.ParseAddr("0.0.0.0")
	ipf := netip.PrefixFrom(addr, bits)
	r.tree.Insert(ipf, struct{}{})

	bits = 32
	addr, _ = netip.ParseAddr("0.0.0.1")
	ipf = netip.PrefixFrom(addr, bits)
	r.tree.Insert(ipf, struct{}{})

	bits = 8
	addr, _ = netip.ParseAddr("10.0.0.0")
	ipf = netip.PrefixFrom(addr, bits)
	r.tree.Insert(ipf, struct{}{})

	bits = 24
	addr, _ = netip.ParseAddr("127.0.0.1")
	ipf = netip.PrefixFrom(addr, bits)
	r.tree.Insert(ipf, struct{}{})

	bits = 12
	addr, _ = netip.ParseAddr("172.16.0.0")
	ipf = netip.PrefixFrom(addr, bits)
	r.tree.Insert(ipf, struct{}{})

	bits = 16
	addr, _ = netip.ParseAddr("192.168.0.0")
	ipf = netip.PrefixFrom(addr, bits)
	r.tree.Insert(ipf, struct{}{})
}

func (r *domainIPRepo) updateDomain(domain string, ips []net.IP) {
	var oldips []net.IP
	r.domainsLock.Lock()
	dn, ok := r.domains[domain]
	if ok {
		dn.lastUpdate = time.Now()
		oldips = dn.ips
		dn.ips = ips
	} else {
		dn = &domainNode{
			name:       domain,
			lastUpdate: time.Now(),
			ips:        ips,
		}

		r.domains[domain] = dn
	}

	r.domainsLock.Unlock()

	r.treeLock.Lock()
	for _, ip := range oldips {
		addr, ok := netip.AddrFromSlice(ip)
		var bits int
		if len(ip) > 4 {
			bits = 128
		} else {
			bits = 32
		}

		if ok {
			ipf := netip.PrefixFrom(addr, bits)
			r.tree.Delete(ipf)
		}
	}

	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		var bits int
		if len(ip) > 4 {
			bits = 128
		} else {
			bits = 32
		}

		if ok {
			ipf := netip.PrefixFrom(addr, bits)
			r.tree.Insert(ipf, struct{}{})
		}
	}
	r.treeLock.Unlock()
}

func (r *domainIPRepo) lookup(ip net.IP) bool {
	r.treeLock.Lock()
	defer r.treeLock.Unlock()

	addr, ok := netip.AddrFromSlice(ip)
	if ok {
		_, ok = r.tree.Lookup(addr)
		return ok
	}

	return false
}
