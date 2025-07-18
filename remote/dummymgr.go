package remote

import "l5proxy_cv/meta"

type DummyMgr struct {
	config MgrConfig
}

func (mgr *DummyMgr) HandleHttpSocks5TCP(conn meta.TCPConn, target *meta.HTTPSocksTargetInfo) {
	defer conn.Close()
}

func (mgr *DummyMgr) Startup() error {
	return nil
}

func (mgr *DummyMgr) Shutdown() error {
	return nil
}

func (mgr *DummyMgr) OnStackReady(_ meta.LocalGivsorNetwork) {
}

func (mgr *DummyMgr) HandleTCP(conn meta.TCPConn) {
	defer conn.Close()

}

func (mgr *DummyMgr) HandleUDP(conn meta.UDPConn, _ []byte) {
	defer conn.Close()
}
