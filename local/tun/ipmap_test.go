package localtun

import (
	"net"
	"testing"
)

func TestXxx(t *testing.T) {
	m := newDomainIPRepo()
	ip := net.ParseIP("64.71.151.11").To4()
	m.updateDomain("sina.com", []net.IP{ip})

	if !m.lookup(ip) {
		t.Fail()
	}

	ip2 := net.ParseIP("64.71.151.12").To4()
	if m.lookup(ip2.To4()) {
		t.Fail()
	}
}

func TestLocalIPs(t *testing.T) {
	m := newDomainIPRepo()
	var ip net.IP
	ip = net.ParseIP("0.0.0.0").To4()
	if !m.lookup(ip) {
		t.Fail()
	}

	ip = net.ParseIP("0.0.0.1").To4()
	if !m.lookup(ip) {
		t.Fail()
	}

	ip = net.ParseIP("127.0.0.8").To4()
	if !m.lookup(ip) {
		t.Fail()
	}

	ip = net.ParseIP("10.1.1.1").To4()
	if !m.lookup(ip) {
		t.Fail()
	}
}
