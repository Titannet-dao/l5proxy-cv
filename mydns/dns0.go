package mydns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	mkdns "github.com/miekg/dns"
	log "github.com/sirupsen/logrus"
)

const (
	alibbDNSServer     = "223.5.5.5:53" // use alibaba dns server
	defaultDialTimeout = 5 * time.Second
)

type AlibbResolver0 struct {
	host      string
	protector func(fd uint64)

	locker     sync.Mutex
	isResolved bool
	ip         net.IP
}

func NewAlibbResolver(host string, protector func(fd uint64)) *AlibbResolver0 {
	return &AlibbResolver0{
		host:       host,
		protector:  protector,
		isResolved: false,
	}
}

func (r *AlibbResolver0) GetHostIP(host string) (net.IP, error) {
	if host != r.host {
		return nil, fmt.Errorf("host not match, expected:%s, input:%s", r.host, host)
	}

	r.locker.Lock()
	defer r.locker.Unlock()

	if r.isResolved {
		return r.ip, nil
	}

	var ip net.IP
	var err error
	ip = net.ParseIP(r.host)
	if ip == nil {
		// not IP form, need DNS query
		ip, err = resolveHost4(r.host, r.protector, alibbDNSServer)
		if err != nil {
			return nil, err
		}
	}

	log.Infof("AlibbResolver0, host:%s, ip:%s", host, ip.String())
	r.ip = ip
	r.isResolved = true
	return ip, nil
}

func resolveHost4(host string, protector func(fd uint64), nameServer string) (net.IP, error) {
	msg := new(mkdns.Msg)
	msg.SetQuestion(mkdns.Fqdn(host), mkdns.TypeA)
	packed, err := msg.Pack() // generate a DNS query packet
	if err != nil {
		return nil, err
	}

	var d *net.Dialer
	if protector != nil {
		d = &net.Dialer{
			Control: func(network, address string, c syscall.RawConn) error {
				c.Control(func(fd uintptr) {
					protector(uint64(fd))
				})
				return nil
			},
		}
	} else {
		d = &net.Dialer{}
	}

	udpConn, err := d.Dial("udp", nameServer)
	if err != nil {
		return nil, err
	}

	defer udpConn.Close()
	n, err := udpConn.Write(packed)
	if err != nil {
		return nil, err
	}

	if n != len(packed) {
		return nil, fmt.Errorf("udp send to dns server length not match:%d != %d", n, len(packed))
	}

	// read reply from DNS server
	buf := make([]byte, 600) // 600 is enough for DNS query reply
	udpConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err = udpConn.Read(buf)
	if err != nil {
		return nil, err
	}

	buf = buf[:n]
	resp := new(mkdns.Msg)
	err = resp.Unpack(buf)
	if err != nil {
		return nil, err
	}

	for _, answer := range resp.Answer {
		t, ok := answer.(*mkdns.A)
		if ok {
			return t.A, nil
		}
	}

	return nil, fmt.Errorf("no A record found in DNS reply")
}

func DialWithProtector(dnsResolver *AlibbResolver0, protector func(uint64),
	ctx context.Context, network, addr string) (net.Conn, error) {

	var addr2 string
	if dnsResolver != nil {
		var hostString, portString string
		if strings.Contains(addr, ":") {
			ss := strings.Split(addr, ":")
			hostString = ss[0]
			portString = ss[1]
		} else {
			hostString = addr
			portString = ""
		}

		ip, err := dnsResolver.GetHostIP(hostString)
		if err != nil {
			return nil, err
		}

		if len(portString) > 0 {
			addr2 = fmt.Sprintf("%s:%s", ip.String(), portString)
		} else {
			addr2 = ip.String()
		}
	} else {
		addr2 = addr
	}

	var d2 *net.Dialer
	if protector != nil {
		d2 = &net.Dialer{
			Timeout: defaultDialTimeout,
			Control: func(network, address string, c syscall.RawConn) error {
				c.Control(func(fd uintptr) {
					protector(uint64(fd))
				})
				return nil
			},
		}
	} else {
		d2 = &net.Dialer{
			Timeout: defaultDialTimeout,
		}
	}

	if ctx != nil {
		return d2.DialContext(ctx, network, addr2)
	} else {
		return d2.Dial(network, addr2)
	}
}
