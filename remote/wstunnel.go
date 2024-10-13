package remote

import (
	"context"
	"encoding/binary"
	"fmt"
	"l5proxy_cv/meta"
	"l5proxy_cv/mydns"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

const (
	wsReadBufSize  = 256 * 1024
	wsWriteBufSize = 256 * 1024

	websocketWriteDealine = 5
)

const (
	cMDNone              = 0
	cMDReqData           = 1
	cMDReqCreated        = 2
	cMDReqClientClosed   = 3
	cMDReqClientFinished = 4
	cMDReqServerFinished = 5
	cMDReqServerClosed   = 6
	cMDDNSReq            = 7
	cMDDNSRsp            = 8
	cMDUDPReq            = 9
	cMDReqDataExt        = 10
)

type WSTunnel struct {
	id int

	websocketURL string
	reqq         *Reqq

	isActivated bool

	protector func(fd uint64)

	wsLock   sync.Mutex
	ws       *websocket.Conn
	waitping int

	cache *UdpCache
	mgr   *Mgr

	dnsResolver *mydns.AlibbResolver0

	withTimestamp bool

	keepaliveLog bool
}

func newTunnel(id int, dnsResolver *mydns.AlibbResolver0, config *MgrConfig) *WSTunnel {
	wst := &WSTunnel{
		id:            id,
		dnsResolver:   dnsResolver,
		websocketURL:  config.WebsocketURL,
		cache:         newUdpCache(),
		withTimestamp: config.WithTimestamp,
		keepaliveLog:  config.KeepaliveLog,
	}

	reqq := newReqq(config.TunnelCap, wst)
	wst.reqq = reqq

	return wst
}

func (tnl *WSTunnel) start() {
	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	if tnl.isActivated {
		return
	}

	tnl.isActivated = true

	go tnl.serveWebsocket()
}

func (tnl *WSTunnel) stop() {
	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	tnl.isActivated = false

	if tnl.ws != nil {
		tnl.ws.Close()
		tnl.ws = nil
	}

	tnl.reqq.cleanup()
	tnl.cache.cleanup()
}

func (tnl *WSTunnel) serveWebsocket() {
	delayfn := func(eCount int) {
		tick := 3 * eCount
		if tick > 30 {
			tick = 30
		} else if tick < 3 {
			tick = 3
		}

		time.Sleep(time.Duration(tick) * time.Second)
	}

	failedConnect := 0
	for tnl.isActivated {
		// connect
		if failedConnect > 0 {
			delayfn(failedConnect)
		}

		conn, err := tnl.dial()
		failedConnect++

		if err != nil {
			log.Errorf("dial %s, %s", tnl.websocketURL, err.Error())
			continue
		}

		tnl.onConnected(conn)

		// read
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Errorf("websocket ReadMessage error: %v", err)
				}
				//log.Errorf("tunnel ws ReadMessage failed %s", err.Error())
				break
			}

			tnl.processWebsocketMsg(message)

			// reset failedConnect
			if failedConnect > 0 {
				failedConnect = 0
			}
		}

		tnl.onDisconnected()
	}
}

func (tnl *WSTunnel) onConnected(conn *websocket.Conn) {
	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	tnl.waitping = 0

	if !tnl.isActivated {
		log.Errorf("tunnel %d onConnected, but isn't activated, close websocket", tnl.id)
		conn.Close()
		tnl.ws = nil

		return
	}

	conn.SetPingHandler(func(data string) error {
		tnl.sendPong([]byte(data))
		return nil
	})

	conn.SetPongHandler(func(data string) error {
		tnl.onPong([]byte(data))
		return nil
	})

	// save for sending
	tnl.ws = conn

	log.Infof("tunnel %d websocket connected", tnl.id)
}

func (tnl *WSTunnel) onDisconnected() {
	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	tnl.waitping = 0

	if tnl.ws != nil {
		tnl.ws.Close()
		tnl.ws = nil
	}

	log.Infof("tunnel %d websocket disconnected", tnl.id)
}

func (tnl *WSTunnel) keepalive() {
	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	if !tnl.isValid() {
		return
	}

	conn := tnl.ws
	if conn == nil {
		return
	}

	if tnl.waitping > 3 {
		log.Errorf("tunnel %d keepalive failed, close websocket", tnl.id)
		tnl.waitping = 0
		conn.Close()
		return
	}

	now := time.Now().UnixMilli()
	data := make([]byte, 9)
	data[0] = 1
	binary.LittleEndian.PutUint64(data[1:], uint64(now))

	conn.SetWriteDeadline(time.Now().Add(websocketWriteDealine * time.Second))
	err := conn.WriteMessage(websocket.PingMessage, data)
	if err != nil {
		log.Errorf("websocket send PingMessage error:%v", err)
	}

	tnl.waitping++
}

func (tnl *WSTunnel) sendPong(data []byte) {
	if !tnl.isActivated {
		return
	}

	conn := tnl.ws
	if conn == nil {
		return
	}

	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	conn.SetWriteDeadline(time.Now().Add(websocketWriteDealine * time.Second))
	err := conn.WriteMessage(websocket.PongMessage, data)
	if err != nil {
		log.Errorf("websocket send PongMessage error:%v", err)
	}
}

func (tnl *WSTunnel) onPong(data []byte) {
	tnl.waitping = 0

	if !tnl.keepaliveLog {
		return
	}

	if len(data) < 1 || data[0] == 0 {
		return
	}

	// dump timestamp
	relayCount := int(data[0]) - 1

	if len(data) != (1 + 8 + relayCount*2) {
		return
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Pinpon[%d] relay count:%d, timestamps:", tnl.id, relayCount))
	offset := 1

	for i := 0; i < relayCount; i++ {
		ts := binary.LittleEndian.Uint16(data[offset:])
		offset = offset + 2
		sb.WriteString(fmt.Sprintf("%d,", ts))
	}

	unixMilli := binary.LittleEndian.Uint64(data[offset:])
	unixMilliNow := time.Now().UnixMilli()

	sb.WriteString(fmt.Sprintf("%d", unixMilliNow-int64(unixMilli)))
	log.Info(sb.String())
}

func (tnl *WSTunnel) send(data []byte) {
	if !tnl.isActivated {
		return
	}

	conn := tnl.ws
	if conn == nil {
		return
	}

	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	conn.SetWriteDeadline(time.Now().Add(websocketWriteDealine * time.Second))
	err := conn.WriteMessage(websocket.BinaryMessage, data)
	if err != nil {
		log.Errorf("websocket WriteMessage error:%v", err)
	}
}

func (tnl *WSTunnel) dial() (*websocket.Conn, error) {
	d := websocket.Dialer{
		ReadBufferSize:   wsReadBufSize,
		WriteBufferSize:  wsWriteBufSize,
		HandshakeTimeout: 5 * time.Second,
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return mydns.DialWithProtector(tnl.dnsResolver, tnl.protector, ctx, network, addr)
		},
	}

	conn, _, err := d.Dial(tnl.websocketURL, nil)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func (tnl *WSTunnel) processWebsocketMsg(msg []byte) {
	if len(msg) < 1 {
		log.Error("WSTunnel.processWebsocketMsg empty msg")
		return
	}

	tnl.waitping = 0

	cmd := msg[0]

	if cmd == cMDUDPReq {
		tnl.onServerUDPData(msg)
	} else {
		tnl.processReqMsg(msg)
	}
}

func (tnl *WSTunnel) processReqMsg(msg []byte) {
	cmd := msg[0]
	idx := binary.LittleEndian.Uint16(msg[1:])
	tag := binary.LittleEndian.Uint16(msg[3:])

	switch cmd {
	case cMDReqData:
		tnl.onServerReqData(idx, tag, msg[5:])
	case cMDReqDataExt:
		tnl.onServerReqDataExt(idx, tag, msg[5:])
	case cMDReqServerFinished:
		tnl.onSeverReqHalfClosed(idx, tag)
	case cMDReqServerClosed:
		tnl.onServerReqClosed(idx, tag)
	case cMDReqCreated:
		tnl.onServerReqCreate(idx, tag, msg[5:])

	}
}

func (tnl *WSTunnel) onServerReqData(idx, tag uint16, msg []byte) {
	req, err := tnl.reqq.get(idx, tag)
	if err != nil {
		log.Debugf("WSTunnel.onServerReqData error:%v", err)
		return
	}

	err = req.onServerData(msg, true)
	if err != nil {
		log.Debugf("WSTunnel.onServerReqData call req.onServerData error:%v", err)
	}
}

func (tnl *WSTunnel) onServerReqDataExt(idx, tag uint16, msg []byte) {
	req, err := tnl.reqq.get(idx, tag)
	if err != nil {
		log.Debugf("WSTunnel.onServerReqDataExt error:%v", err)
		return
	}

	tnl.dumpDataExtraTimestamp(req, msg)

	cut := len(msg) - (8 + 4*2)
	err = req.onServerData(msg[0:cut], false)
	if err != nil {
		log.Debugf("WSTunnel.onServerReqDataExt call req.onServerData error:%v", err)
	}
}

func (tnl *WSTunnel) dumpDataExtraTimestamp(req *Req, msg []byte) {
	ctx := req.ctx
	if ctx == nil {
		return
	}

	extraBytesLen := 8 + 4*2
	size := len(msg)

	if size <= extraBytesLen {
		log.Errorf("tunnel %d dumpTimestamp failed, size %d not enough", tnl.id, size)
		return
	}

	offset := size - extraBytesLen
	unixMilli := binary.LittleEndian.Uint64(msg[offset:])
	offset = offset + 8

	unixMilliNow := time.Now().UnixMilli()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s]-->[%s],size:%d, timestamps[ms]:", ctx.To, ctx.From, size-extraBytesLen))

	for i := 0; i < 4; i++ {
		ts := binary.LittleEndian.Uint16(msg[offset:])
		if ts == 0 {
			break
		}

		offset = offset + 2
		sb.WriteString(fmt.Sprintf("%d,", ts))
	}

	sb.WriteString(fmt.Sprintf("%d", unixMilliNow-int64(unixMilli)))
	log.Info(sb.String())
}

func (tnl *WSTunnel) onSeverReqHalfClosed(idx, tag uint16) {
	req, err := tnl.reqq.get(idx, tag)
	if err != nil {
		log.Debugf("WSTunnel.onSeverReqHalfClosed error:%v", err)
		return
	}

	req.onSeverHalfClosed()
}

func (tnl *WSTunnel) onServerReqClosed(idx, tag uint16) {
	tnl.freeReq(idx, tag)
}

func (tnl *WSTunnel) onServerReqCreate(idx, tag uint16, message []byte) {
	src := parseTCPAddrss(message[0:])

	srciplen := net.IPv6len
	if src.IP.To4() != nil {
		srciplen = net.IPv4len
	}

	dst := parseTCPAddrss(message[3+srciplen:])

	req, err := tnl.reqq.allocForReverseProxy(idx, tag)
	if err != nil {
		log.Info("onServerReqCreate, alloc req failed:", err)
		return
	}

	log.Info("tcp proxy to ", src.String())

	srcAddr, err := net.ResolveTCPAddr("tcp", src.String())
	if err != nil {
		log.Errorf("onServerReqCreate resolveTCPAddr %s failed %s", src.String(), err.Error())
		return
	}

	conn, err := tnl.newTCP(srcAddr, dst)
	if err != nil {
		log.Errorf("onServerReqCreate newTCP %s failed %s", src.String(), err.Error())
		tnl.onClientTerminate(req.idx, req.tag)
		return
	}

	req.conn = conn
	go req.proxy()
}

func (tnl *WSTunnel) isValid() bool {
	return tnl.isActivated && tnl.ws != nil
}

func (tnl *WSTunnel) onClientTerminate(idx uint16, tag uint16) {
	buf := make([]byte, 5)
	buf[0] = cMDReqClientClosed
	binary.LittleEndian.PutUint16(buf[1:], idx)
	binary.LittleEndian.PutUint16(buf[3:], tag)

	tnl.send(buf)

	tnl.freeReq(idx, tag)
}

func (tnl *WSTunnel) freeReq(idx, tag uint16) {
	err := tnl.reqq.free(idx, tag)
	if err != nil {
		log.Debugf("WSTunnel.freeReq, get req failed:%v", err)
		return
	}
}

func (tnl *WSTunnel) onClientHalfClosed(idx uint16, tag uint16) {
	buf := make([]byte, 5)
	buf[0] = cMDReqClientFinished
	binary.LittleEndian.PutUint16(buf[1:], idx)
	binary.LittleEndian.PutUint16(buf[3:], tag)

	tnl.send(buf)
}

func (tnl *WSTunnel) onClientReqData(idx uint16, tag uint16, data []byte) {
	extraBytesLen := 0
	if tnl.withTimestamp {
		extraBytesLen = 8 + 4*2
	}

	cmdAndDataLen := 5 + len(data)
	buf := make([]byte, cmdAndDataLen+extraBytesLen)

	if tnl.withTimestamp {
		buf[0] = cMDReqDataExt
	} else {
		buf[0] = cMDReqData
	}

	binary.LittleEndian.PutUint16(buf[1:], idx)
	binary.LittleEndian.PutUint16(buf[3:], tag)
	copy(buf[5:], data)

	if tnl.withTimestamp {
		// write timestamp
		timestamp := uint64(time.Now().UnixMilli())
		binary.LittleEndian.PutUint64(buf[cmdAndDataLen:], timestamp)
	}

	tnl.send(buf)
}

func (tnl *WSTunnel) acceptTCPConn(conn meta.TCPConn) error {
	req, err := tnl.reqq.alloc(conn)
	if err != nil {
		return err
	}

	tnl.onClientCreateByID(conn.ID(), req)

	req.proxy()

	return nil
}

func (tnl *WSTunnel) onClientCreateByID(id *stack.TransportEndpointID, req *Req) {
	addr := id.LocalAddress
	port := id.LocalPort

	iplen := addr.Len()

	buf := make([]byte, 8+iplen)
	buf[0] = cMDReqCreated
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)

	if iplen > 4 {
		// ipv6
		buf[5] = 2
		src := addr.As16()
		copy(buf[6:], src[:])
	} else {
		buf[5] = 0
		src := addr.As4()
		copy(buf[6:], src[:])
	}

	binary.LittleEndian.PutUint16(buf[6+iplen:], uint16(port))

	tnl.send(buf)
}

func (tnl *WSTunnel) acceptHttpSocks5TCPConn(conn meta.TCPConn, target *meta.HTTPSocksTargetInfo) error {
	req, err := tnl.reqq.alloc(conn)
	if err != nil {
		return err
	}

	tnl.onClientCreateByDomain(req, target)

	if len(target.ExtraBytes) > 0 {
		// send extra data
		tnl.onClientReqData(req.idx, req.tag, target.ExtraBytes)
	}

	ctx := &ReqContext{
		From: conn.RemoteAddr().String(),
		To:   target.DomainName,
	}
	req.ctx = ctx

	// read data from 'conn'
	// NOTE: we need not to start a new goroutine here
	req.proxy()

	return nil
}

func (tnl *WSTunnel) onClientCreateByDomain(req *Req, target *meta.HTTPSocksTargetInfo) {
	domainLen := len(target.DomainName)

	buf := make([]byte, 9+domainLen)
	buf[0] = cMDReqCreated
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)

	buf[5] = 1 // domain type
	buf[6] = byte(domainLen)
	copy(buf[7:], []byte(target.DomainName))
	binary.LittleEndian.PutUint16(buf[7+domainLen:], uint16(target.Port))

	tnl.send(buf)
}

func (tnl *WSTunnel) acceptUDPConn(conn meta.UDPConn, extra []byte) error {
	src := &net.UDPAddr{Port: int(conn.ID().RemotePort), IP: conn.ID().RemoteAddress.AsSlice()}
	dst := &net.UDPAddr{Port: int(conn.ID().LocalPort), IP: conn.ID().LocalAddress.AsSlice()}

	log.Infof("acceptUDPConn src %s dst %s", src.String(), dst.String())

	ustub := tnl.cache.get(src, dst)
	if ustub != nil {
		return fmt.Errorf("conn src %s dst %s already exist", src.String(), dst.String())
	}

	ustub = newUdpStub(tnl, conn)
	tnl.cache.add(ustub)
	ustub.proxy(extra)

	return nil
}

func (tnl *WSTunnel) onServerUDPData(msg []byte) error {
	src := parseUDPAddrss(msg[1:])

	srcipLen := net.IPv6len
	if src.IP.To4() != nil {
		srcipLen = net.IPv4len
	}

	dst := parseUDPAddrss(msg[1+3+srcipLen:])

	dstipLen := net.IPv6len
	if dst.IP.To4() != nil {
		dstipLen = net.IPv4len
	}

	log.Debugf("onServerUDPData src %s dst %s", src.String(), dst.String())

	ustub := tnl.cache.get(src, dst)
	if ustub == nil {
		conn, err := tnl.newUDP(src, dst)
		if err != nil {
			log.Errorf("onServerUDPData new UDPConn src %s dst %s failed, %s", src.String(), dst.String(), err.Error())
			return nil
		}

		ustub = newUdpStub(tnl, conn)
		tnl.cache.add(ustub)
		go ustub.proxy(nil)

		log.Infof("onServerUDPData, new UDPConn src %s dst %s for reverse proxy", src.String(), dst.String())
	}

	// 7 = cmd + ipType1 + port1 + ipType2 + port2
	skip := 7 + srcipLen + dstipLen
	return ustub.writeTo(msg[skip:], src)
}

func (tnl *WSTunnel) onClientUDPData(msg []byte, src, dst *net.UDPAddr) error {
	log.Debugf("onClientUDPData src %s, dst %s", src, dst)
	srcAddrBuf := writeUDPAddress(src)
	dstAddrBuf := writeUDPAddress(dst)

	buf := make([]byte, 1+len(srcAddrBuf)+len(dstAddrBuf)+len(msg))

	buf[0] = byte(cMDUDPReq)
	copy(buf[1:], srcAddrBuf)
	copy(buf[1+len(srcAddrBuf):], dstAddrBuf)
	copy(buf[1+len(srcAddrBuf)+len(dstAddrBuf):], msg)

	tnl.send(buf)
	return nil
}

func (tnl *WSTunnel) newUDP(src, dst *net.UDPAddr) (meta.UDPConn, error) {
	if tnl.mgr.localGvisor == nil {
		return nil, fmt.Errorf("localGvisor == nil")
	}

	id := &stack.TransportEndpointID{
		LocalPort:     uint16(dst.Port),
		LocalAddress:  tcpip.AddrFromSlice(dst.IP),
		RemotePort:    uint16(src.Port),
		RemoteAddress: tcpip.AddrFromSlice(src.IP),
	}

	newUDP4, err := tnl.mgr.localGvisor.NewUDP4(id)
	if err != nil {
		return nil, fmt.Errorf("NewUDP4 failed:%s", err)
	}

	return newUDP4, nil
}

func (tnl *WSTunnel) newTCP(src *net.TCPAddr, dst *net.TCPAddr) (meta.TCPConn, error) {
	if tnl.mgr.localGvisor == nil {
		return nil, fmt.Errorf("localGvisor == nil")
	}

	id := &stack.TransportEndpointID{
		RemotePort: uint16(src.Port),
		LocalPort:  uint16(dst.Port),
	}

	if src.IP.To4() != nil {
		id.RemoteAddress = tcpip.AddrFromSlice(src.IP.To4())
	} else {
		id.RemoteAddress = tcpip.AddrFromSlice(src.IP.To16())
	}

	if dst.IP.To4() != nil {
		id.LocalAddress = tcpip.AddrFromSlice(dst.IP.To4())
	} else {
		id.LocalAddress = tcpip.AddrFromSlice(dst.IP.To16())
	}

	newTCP4, err := tnl.mgr.localGvisor.NewTCP4(id)
	if err != nil {
		return nil, fmt.Errorf("NewTCP4 failed:%s", err)
	}

	return newTCP4, nil
}

func writeUDPAddress(addrss *net.UDPAddr) []byte {
	// 3 = iptype(1) + port(2)
	buf := make([]byte, 3+len(addrss.IP))
	// add port
	binary.LittleEndian.PutUint16(buf[0:], uint16(addrss.Port))
	// set ip type
	if len(addrss.IP) > net.IPv4len {
		// ipv6
		buf[2] = 2
	} else {
		// ipv4
		buf[2] = 0
	}

	copy(buf[3:], addrss.IP)
	return buf
}

func parseTCPAddrss(msg []byte) *net.TCPAddr {
	addr := parseAddress("tcp", msg)
	return addr.(*net.TCPAddr)
}

func parseUDPAddrss(msg []byte) *net.UDPAddr {
	addr := parseAddress("udp", msg)
	return addr.(*net.UDPAddr)
}

func parseAddress(network string, msg []byte) net.Addr {
	offset := 0
	port := binary.LittleEndian.Uint16(msg[offset:])
	offset += 2

	ipType := msg[offset]
	offset += 1

	var ip []byte = nil
	switch ipType {
	case 0:
		// ipv4
		ip = make([]byte, net.IPv4len)
		copy(ip, msg[offset:offset+net.IPv4len])
	case 2:
		// ipv6
		ip = make([]byte, net.IPv6len)
		copy(ip, msg[offset:offset+net.IPv6len])
	}

	switch network {
	case "tcp":
		return &net.TCPAddr{IP: ip, Port: int(port)}
	case "udp":
		return &net.UDPAddr{IP: ip, Port: int(port)}
	default:
		log.Fatalf("network %s not support", network)
	}
	return nil
}

// func setSocketMark(fd, mark int) error {
// 	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_MARK, mark); err != nil {
// 		return os.NewSyscallError("failed to set mark", err)
// 	}
// 	return nil
// }
