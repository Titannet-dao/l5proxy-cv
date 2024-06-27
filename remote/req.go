package remote

import (
	"net"

	log "github.com/sirupsen/logrus"
)

type Req struct {
	isUsed bool
	idx    uint16
	tag    uint16

	owner *WSTunnel

	conn *net.TCPConn
}

func newReq(t *WSTunnel, idx uint16) *Req {
	return &Req{
		owner:  t,
		idx:    idx,
		tag:    0,
		isUsed: false,
	}
}

// free should be called by Reqq only, lock() must be called first
func (r *Req) free() {
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
}

func (r *Req) onServerData(data []byte) error {
	// if write failed, return an error, outer should free this request
	conn := r.conn
	if conn != nil {
		wrote := 0
		l := len(data)
		for {
			n, err := conn.Write(data[wrote:])
			if err != nil {
				return err
			}

			wrote = wrote + n
			if wrote == l {
				break
			}
		}
	} else {
		log.Errorf("onServerData error: null conn, idx:%d", r.idx)
	}

	return nil
}

func (r *Req) onSeverHalfClosed() {
	conn := r.conn
	if conn != nil {
		err := conn.CloseWrite()
		if err != nil {
			log.Errorf("onSeverHalfClosed error: %v, idx:%d", err, r.idx)
		}
	}
}

func (r *Req) proxy() {
	conn := r.conn

	if conn == nil {
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)

		if !r.isUsed {
			// request is free!
			log.Infof("proxy read, request is free, discard data, len:%d", n)
			break
		}

		// owner never be null
		owner := r.owner

		if err != nil {
			// log.Println("proxy read failed:", err)
			owner.onClientTerminate(r.idx, r.tag)
			break
		}

		if n == 0 {
			// log.Println("proxy read, server half close")
			owner.onClientHalfClosed(r.idx, r.tag)
			break
		}

		owner.onClientReqData(r.idx, r.tag, buf[:n])
	}
}
