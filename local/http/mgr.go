package localhttp

import (
	"bufio"
	"context"
	"fmt"
	"l5proxy_cv/meta"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

type LocalConfig struct {
	Address string

	TransportHandler meta.HTTPSocks5TransportHandler
}

type Mgr struct {
	cfg LocalConfig

	defaultHandler *requestHandler
	server         *http.Server
}

type httpconn struct {
	*net.TCPConn
}

func (hc httpconn) ID() *stack.TransportEndpointID {
	return nil
}

type requestHandler struct {
	mgr *Mgr
}

func getTargetPortOrDefault(uurl *url.URL) int {
	portStr := uurl.Port()
	if len(portStr) > 0 {
		port, _ := strconv.Atoi(portStr)
		return port
	}

	switch uurl.Scheme {
	case "http":
		return 80
	case "https":
		return 443
	case "ws":
		return 80
	case "wss":
		return 443
	}

	return 80
}

func drainBuffered(bufrw *bufio.ReadWriter) ([]byte, error) {
	buffered := bufrw.Reader.Buffered()
	extraBytes := make([]byte, buffered)
	n, err := bufrw.Reader.Read(extraBytes)
	if err != nil {
		return nil, fmt.Errorf("localhttp.drainBuffered read bufrw failed:%s", err)
	}

	if n != buffered {
		return nil, fmt.Errorf("localhttp.ServeHTTP read bufrw not match, expected:%d, read:%d", buffered, n)
	}

	return extraBytes, nil
}

func rebuildHTTPHeaders(r *http.Request) strings.Builder {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s %s", r.Method, r.URL.Path))
	if len(r.URL.RawQuery) > 0 {
		b.WriteString(fmt.Sprintf("?%s", r.URL.RawQuery))
	}

	if len(r.URL.RawFragment) > 0 {
		b.WriteString(fmt.Sprintf("#%s", r.URL.RawFragment))
	}

	b.WriteString(fmt.Sprintf(" HTTP/%d.%d\r\n", r.ProtoMajor, r.ProtoMinor))

	for k, v := range r.Header {
		for _, vv := range v {
			b.WriteString(fmt.Sprintf("%s: %s\r\n", k, vv))
		}
	}
	b.WriteString("\r\n")
	return b
}

func (rh *requestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "HTTP Response doesn't support hijacking", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var handled = false
	defer func() {
		if !handled {
			conn.Close()
		}
	}()

	targetAddress := &meta.HTTPSocksTargetAddress{}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		http.Error(w, "not tcp connection", http.StatusInternalServerError)
		return
	}

	uurl := r.URL
	targetAddress.DomainName = uurl.Hostname()
	targetAddress.Port = getTargetPortOrDefault(uurl)

	var extraBytes []byte

	if r.Method == http.MethodConnect {
		// CONNECT request
		// write a reply to client
		bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n" +
			"Proxy-agent: l5proxy\r\n" +
			"\r\n")
		bufrw.Flush()
		buffered := bufrw.Reader.Buffered()
		if buffered > 0 {
			extraBytes, err = drainBuffered(bufrw)
			if err != nil {
				log.Errorf("localhttp.ServeHTTP error: %s", err)
				return
			}
		}
	} else {
		// normal request
		builder := rebuildHTTPHeaders(r)
		buffered := bufrw.Reader.Buffered()
		extraBytes = make([]byte, builder.Len()+buffered)
		copy(extraBytes, []byte(builder.String()))

		if buffered > 0 {
			var bufrwBytes []byte
			bufrwBytes, err = drainBuffered(bufrw)
			if err != nil {
				log.Errorf("localhttp.ServeHTTP error: %s", err)
				return
			}
			copy(extraBytes[builder.Len():], bufrwBytes)
		}
	}

	targetAddress.ExtraBytes = extraBytes
	thandler := rh.mgr.cfg.TransportHandler
	if thandler != nil {
		thandler.HandleHttpSocks5TCP(httpconn{TCPConn: tcpConn}, targetAddress)
		handled = true
	}
}

func NewMgr(cfg *LocalConfig) meta.Local {
	mgr := &Mgr{
		cfg: *cfg,
	}

	return mgr
}

func (mgr *Mgr) Name() string {
	return "httpmode"
}

func (mgr *Mgr) Startup() error {
	if mgr.server != nil {
		return fmt.Errorf("localhttp.Mgr already startup")
	}

	mgr.defaultHandler = &requestHandler{
		mgr: mgr,
	}

	mgr.server = &http.Server{Addr: mgr.cfg.Address, Handler: mgr.defaultHandler}
	go mgr.serveHTTP()

	return nil
}

func (mgr *Mgr) Shutdown() error {
	if mgr.server == nil {
		return fmt.Errorf("localhttp.Mgr isn't running")
	}

	ctx, cancel := context.WithTimeout(context.TODO(), 3*time.Second)
	defer cancel()
	err := mgr.server.Shutdown(ctx)
	if err != nil {
		log.Errorf("localhttp.Mgr shutdown http server failed:%s", err)
	}

	return nil
}

func (mgr *Mgr) serveHTTP() {
	err := mgr.server.ListenAndServe()
	if err != nil {
		log.Errorf("localhttp.Mgr serveHTTP error:%s", err)
	}
}
