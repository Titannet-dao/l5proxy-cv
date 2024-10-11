package localtun

import (
	"l5proxy_cv/meta"
	"l5proxy_cv/mydns"

	mkdns "github.com/miekg/dns"
	log "github.com/sirupsen/logrus"
)

func (mgr *Mgr) handleDNSQuery(conn meta.UDPConn) {
	handled := false
	defer func() {
		if !handled {
			conn.Close()
		}
	}()

	data := make([]byte, 600) // 600 is enough for DNS query
	n, err := conn.Read(data)
	if err != nil {
		log.Errorf("localtun.Mgr handleDNSQuery error:%s", err)
		return
	}

	data = data[0:n]

	query := new(mkdns.Msg)
	err = query.Unpack(data)
	if err != nil {
		log.Errorf("localtun.Mgr handleDNSQuery error:%s", err)
		return
	}

	if len(query.Question) == 0 {
		log.Error("localtun.Mgr handleDNSQuery question field is empty")
		return
	}

	domainName := query.Question[0].Name
	domainName = domainName[0 : len(domainName)-1] // remove last '.'
	bypass := mgr.cfg.BypassHandler.BypassAbleDomain(domainName)
	if !bypass {
		mgr.cfg.TransportHandler.HandleUDP(conn, data)
		handled = true
		return
	}

	reply, err := mydns.AlibbDNSQuery(data, mgr.cfg.Protector)
	if err != nil {
		log.Errorf("localtun.Mgr handleDNSQuery alibbDNSQuery failed:%s", reply)
		return
	}

	mgr.catchDNSResult(reply)
	n, err = conn.Write(reply)
	if err != nil {
		log.Errorf("localtun.Mgr handleDNSQuery write reply failed:%s", reply)
		return
	}

	if n != len(reply) {
		log.Errorf("localtun.Mgr handleDNSQuery write reply expected %d, actual write:%d", len(reply), n)
		return
	}
}
