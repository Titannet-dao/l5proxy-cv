package remote

import (
	"fmt"
	"net"
	"sync"
)

// Reqq request queue
type Reqq struct {
	l sync.Mutex

	array []*Req
	ids   []uint16
}

func newReqq(cap int, t *WSTunnel) *Reqq {
	reqq := &Reqq{}

	reqq.array = make([]*Req, cap)
	reqq.ids = make([]uint16, cap)
	for i := 0; i < cap; i++ {
		reqq.array[i] = newReq(t, uint16(i))
		reqq.ids[i] = uint16(i)
	}

	return reqq
}

func (q *Reqq) alloc(conn *net.TCPConn) (*Req, error) {
	q.l.Lock()
	defer q.l.Unlock()

	len := len(q.ids)
	if len < 1 {
		return nil, fmt.Errorf("all reqs are used")
	}

	idx := q.ids[len-1]
	q.ids = q.ids[0 : len-1]
	req := q.array[idx]

	if req.isUsed {
		return nil, fmt.Errorf("alloc, req %d is in used", idx)
	}

	req.tag = req.tag + 1
	req.isUsed = true
	req.conn = conn

	return req, nil
}

func (q *Reqq) free(idx uint16, tag uint16) error {
	if idx >= uint16(len(q.array)) {
		return fmt.Errorf("free, idx %d >= len %d", idx, uint16(len(q.array)))
	}

	q.l.Lock()
	defer q.l.Unlock()

	req := q.array[idx]

	if !req.isUsed {
		return fmt.Errorf("free, req %d:%d is in not used", idx, tag)
	}

	if req.tag != tag {
		return fmt.Errorf("free, req %d:%d is in not match tag %d", idx, tag, req.tag)
	}

	// log.Printf("reqq free req %d:%d", idx, tag)

	req.free()
	req.tag++
	req.isUsed = false

	q.ids = append(q.ids, idx)

	return nil
}

func (q *Reqq) get(idx uint16, tag uint16) (*Req, error) {
	if idx >= uint16(len(q.array)) {
		return nil, fmt.Errorf("get, idx %d >= len %d", idx, uint16(len(q.array)))
	}

	q.l.Lock()
	defer q.l.Unlock()

	req := q.array[idx]

	if !req.isUsed {
		return nil, fmt.Errorf("get, req %d:%d is not in used", idx, tag)
	}

	if req.tag != tag {
		return nil, fmt.Errorf("get, req %d:%d tag not match %d", idx, req.tag, tag)
	}

	return req, nil
}

func (q *Reqq) cleanup() {
	q.l.Lock()
	defer q.l.Unlock()

	for _, r := range q.array {
		if r.isUsed {
			q.free(r.idx, r.tag)
		}
	}
}
