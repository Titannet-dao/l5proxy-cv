package remote

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	readBufSize  = 16 * 1024
	writeBufSize = 16 * 1024
)

const (
	cMDNone              = 0
	cMDReqBegin          = 1
	cMDReqData           = 1
	cMDReqCreated        = 2
	cMDReqClientClosed   = 3
	cMDReqClientFinished = 4
	cMDReqServerFinished = 5
	cMDReqServerClosed   = 6
	cMDReqEnd            = 7
	cMDDNSReq            = 7
	cMDDNSRsp            = 8
)

type WSTunnel struct {
	websocketURL string
	reqq         *Reqq

	isActivated bool

	protector func(fd uint64)

	wsLock   sync.Mutex
	ws       *websocket.Conn
	waitping int
}

func newTunnel(websocketURL string, reqCap int) *WSTunnel {
	wst := &WSTunnel{
		websocketURL: websocketURL,
	}

	reqq := newReqq(reqCap, wst)
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
}

func (tnl *WSTunnel) serveWebsocket() {
	delayfn := func(eCount int) {
		tick := 3 * eCount
		if tick > 15 {
			tick = 15
		} else if tick < 3 {
			tick = 3
		}

		time.Sleep(time.Duration(tick) * time.Second)
	}

	failedConnect := 0
	for tnl.isActivated {
		// connect
		conn, err := tnl.dail()
		if err != nil {
			failedConnect++
			delayfn(failedConnect)
			continue
		}

		tnl.onConnected(conn)

		failedConnect = 0

		// read
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Errorf("websocket ReadMessage error: %v", err)
				}
				break
			}

			tnl.processWebsocketMsg(message)
		}

		tnl.onDisconnected()
	}
}

func (tnl *WSTunnel) onConnected(conn *websocket.Conn) {
	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	if !tnl.isActivated {
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
}

func (tnl *WSTunnel) onDisconnected() {
	tnl.wsLock.Lock()
	defer tnl.wsLock.Unlock()

	tnl.ws.Close()
	tnl.ws = nil
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

	now := time.Now().Unix()
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, uint64(now))

	if tnl.waitping > 3 {
		conn.Close()
		return
	}

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

	err := conn.WriteMessage(websocket.PongMessage, data)
	if err != nil {
		log.Errorf("websocket send PongMessage error:%v", err)
	}
}

func (tnl *WSTunnel) onPong(_ []byte) {
	tnl.waitping = 0
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

	err := conn.WriteMessage(websocket.BinaryMessage, data)
	if err != nil {
		log.Errorf("websocket WriteMessage error:%v", err)
	}
}

func (tnl *WSTunnel) dail() (*websocket.Conn, error) {
	d := websocket.Dialer{
		ReadBufferSize:   readBufSize,
		WriteBufferSize:  writeBufSize,
		HandshakeTimeout: 5 * time.Second,
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d *net.Dialer
			if tnl.protector != nil {
				d = &net.Dialer{
					Control: func(network, address string, c syscall.RawConn) error {
						c.Control(func(fd uintptr) {
							tnl.protector(uint64(fd))
						})
						return nil
					},
				}
			} else {
				d = &net.Dialer{}
			}

			return d.DialContext(ctx, network, addr)
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

	cmd := msg[0]
	if cmd >= cMDReqBegin && cmd < cMDReqEnd {
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
	case cMDReqServerFinished:
		tnl.onSeverReqHalfClosed(idx, tag)
	case cMDReqServerClosed:
		tnl.onServerReqClosed(idx, tag)
	}
}

func (tnl *WSTunnel) onServerReqData(idx, tag uint16, msg []byte) {
	req, err := tnl.reqq.get(idx, tag)
	if err != nil {
		log.Errorf("WSTunnel.onServerReqData error:%v", err)
		return
	}

	err = req.onServerData(msg)
	if err != nil {
		log.Errorf("WSTunnel.onServerReqData call req.onServerData error:%v", err)
	}
}

func (tnl *WSTunnel) onSeverReqHalfClosed(idx, tag uint16) {
	req, err := tnl.reqq.get(idx, tag)
	if err != nil {
		log.Errorf("WSTunnel.onServerReqData error:%v", err)
		return
	}

	req.onSeverHalfClosed()
}

func (tnl *WSTunnel) onServerReqClosed(idx, tag uint16) {
	tnl.freeReq(idx, tag)
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
		log.Errorf("WSTunnel.freeReq, get req failed:%v", err)
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
	buf := make([]byte, 5+len(data))
	buf[0] = cMDReqData
	binary.LittleEndian.PutUint16(buf[1:], idx)
	binary.LittleEndian.PutUint16(buf[3:], tag)
	copy(buf[5:], data)

	tnl.send(buf)
}

func (tnl *WSTunnel) onClientCreate(conn *net.TCPConn, idx, tag uint16) error {
	addr := conn.RemoteAddr()
	var ip net.IP
	port := 0

	switch addr2 := addr.(type) {
	case *net.UDPAddr:
		return fmt.Errorf("tcp conn should not have udp address type")
	case *net.TCPAddr:
		ip = addr2.IP
		port = addr2.Port
	}

	iplen := len(ip)

	buf := make([]byte, 8+iplen)
	buf[0] = cMDReqCreated
	binary.LittleEndian.PutUint16(buf[1:], idx)
	binary.LittleEndian.PutUint16(buf[3:], tag)

	if iplen > 4 {
		// ipv6
		buf[5] = 2
	} else {
		buf[5] = 0
	}

	copy(buf[6:], ip)
	binary.LittleEndian.PutUint16(buf[6+iplen:], uint16(port))

	tnl.send(buf)

	return nil
}

func (tnl *WSTunnel) acceptTCPConn(conn *net.TCPConn) error {
	req, err := tnl.reqq.alloc(conn)
	if err != nil {
		return err
	}

	err = tnl.onClientCreate(conn, req.idx, req.tag)

	if err != nil {
		return err
	}

	// start a new goroutine to read data from 'conn'
	go req.proxy()

	return nil
}

func (tnl *WSTunnel) acceptUDPConn(_ *net.UDPConn) error {
	// TODO:
	return fmt.Errorf("udp conn is not supported yet")
}
