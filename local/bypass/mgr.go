package localbypass

import (
	"bufio"
	"fmt"
	"io"
	"l5proxy_cv/meta"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

type LocalConfig struct {
	WhitelistURL string
}

type bypassconn struct {
	*net.TCPConn
}

func (bp bypassconn) ID() *stack.TransportEndpointID {
	return nil
}

type Mgr struct {
	cfg LocalConfig

	isActivated bool

	whitelist map[string]struct{}
}

func NewMgr(cfg *LocalConfig) meta.Local {
	mgr := &Mgr{
		cfg: *cfg,

		whitelist: make(map[string]struct{}),
	}

	return mgr
}

func (mgr *Mgr) Name() string {
	return "bypassmode"
}

func (mgr *Mgr) Startup() error {
	if mgr.isActivated {
		return fmt.Errorf("bypass mode already startup")
	}

	err := mgr.loadWhitelist()
	if err != nil {
		return err
	}

	mgr.isActivated = true

	log.Info("bypass mode startup")
	return nil
}

func (mgr *Mgr) Shutdown() error {
	if !mgr.isActivated {
		return fmt.Errorf("bypass mode is not runnning")
	}

	mgr.isActivated = false
	log.Info("bypass mode shutdown")
	return nil
}

func (mgr *Mgr) loadWhitelist() error {
	rsp, err := http.Get(mgr.cfg.WhitelistURL)
	if err != nil {
		return err
	}

	defer rsp.Body.Close()

	if rsp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(rsp.Body)
		if err != nil {
			return err
		}

		bodyString := string(bodyBytes)
		reader := bufio.NewReader(strings.NewReader(bodyString))
		for {
			linebytes, isPrefix, err := reader.ReadLine()
			if err != nil {
				if err == io.EOF {
					break
				}

				return err
			}

			if isPrefix {
				return fmt.Errorf("loadWhitelist failed, underlying buffer is too small")
			}

			domain := strings.TrimSpace(string(linebytes))
			if len(domain) > 0 {
				mgr.whitelist[domain] = struct{}{}
			}
		}

		log.Infof("localbypass.Mgr load whilte domain name count: %d", len(mgr.whitelist))
		return nil
	} else {
		return fmt.Errorf("rsp status code %d != 200", rsp.StatusCode)
	}
}

func (mgr *Mgr) HandleHttpSocks5TCP(conn meta.TCPConn, address *meta.HTTPSocksTargetAddress) {
	defer conn.Close()

	d := net.Dialer{Timeout: 5 * time.Second}
	addr := fmt.Sprintf("%s:%d", address.DomainName, address.Port)
	conn2, err := d.Dial("tcp", addr)
	if err != nil {
		log.Errorf("localbypass.Mgr dial %s failed:%s", addr, err)
		return
	}

	defer conn2.Close()

	if len(address.ExtraBytes) > 0 {
		n, err := conn2.Write(address.ExtraBytes)
		if err != nil {
			log.Errorf("localbypass.Mgr write extra bytes to %s failed:%s", addr, err)
			return
		}

		if n != len(address.ExtraBytes) {
			log.Errorf("localbypass.Mgr write extra bytes to %s failed, expected %d, actual %d", addr, len(address.ExtraBytes), n)
			return
		}
	}

	conn3, ok := conn2.(*net.TCPConn)
	if !ok {
		log.Error("localbypass.Mgr convert conn to TCPConn failed")
		return
	}

	wg := &sync.WaitGroup{}
	wg.Add(2)

	bc := bypassconn{
		TCPConn: conn3,
	}

	go mgr.pipeTcpSocket(conn, bc, wg)
	go mgr.pipeTcpSocket(bc, conn, wg)

	log.Infof("proxy[bypass/tcp] to %s", addr)
	wg.Wait()
}

func (mgr *Mgr) pipeTcpSocket(from meta.TCPConn, to meta.TCPConn, wg *sync.WaitGroup) {
	buf := make([]byte, 4096)
	for {
		n, err := from.Read(buf)

		if err != nil {
			// log.Println("proxy read failed:", err)
			to.Close()
			break
		}

		if n == 0 {
			// log.Println("proxy read, server half close")
			to.CloseWrite()
			break
		}

		to.SetWriteDeadline(time.Now().Add(10 * time.Second))
		n1, err := to.Write(buf[0:n])
		if n1 != n {
			to.Close()
			break
		}

		if err != nil {
			to.Close()
			break
		}
	}

	wg.Done()
}

func (mgr *Mgr) BypassAble(domainName string) bool {
	if !mgr.isActivated {
		return false
	}

	if mgr.isLocalIP(domainName) {
		return true
	}

	if mgr.isDomainInWhitelist(domainName) {
		return true
	}

	return false
}

func (mgr *Mgr) isLocalIP(domainName string) bool {
	// TODO: ipv6
	return isStringLocalIP4(domainName)
}

func (mgr *Mgr) isDomainInWhitelist(domainName string) bool {
	return isDomainIn(domainName, mgr.whitelist)
}
