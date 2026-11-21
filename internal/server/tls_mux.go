package server

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"wdev/internal/auth"
)

type peekConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

type chanListener struct {
	addr   net.Addr
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newChanListener(addr net.Addr) *chanListener {
	return &chanListener{
		addr:   addr,
		conns:  make(chan net.Conn, 128),
		closed: make(chan struct{}),
	}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		if c == nil {
			return nil, net.ErrClosed
		}
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) enqueue(c net.Conn) bool {
	select {
	case <-l.closed:
		_ = c.Close()
		return false
	case l.conns <- c:
		return true
	}
}

func (l *chanListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *chanListener) Addr() net.Addr {
	return l.addr
}

type tlsCertCache struct {
	mu       sync.Mutex
	certPath string
	keyPath  string
	certMod  time.Time
	keyMod   time.Time
	pair     *tls.Certificate
}

func (c *tlsCertCache) getCertificate(certPath, keyPath string) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stC, errC := os.Stat(certPath)
	stK, errK := os.Stat(keyPath)
	if errC != nil || errK != nil {
		return nil, fmt.Errorf("sertifikat TLS panel tidak tersedia (%s)", certPath)
	}
	if c.pair != nil && c.certPath == certPath && c.keyPath == keyPath &&
		stC.ModTime().Equal(c.certMod) && stK.ModTime().Equal(c.keyMod) {
		return c.pair, nil
	}

	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	c.certPath = certPath
	c.keyPath = keyPath
	c.certMod = stC.ModTime()
	c.keyMod = stK.ModTime()
	c.pair = &pair
	return c.pair, nil
}

func (s *Server) panelExtraSANHosts() []string {
	var hosts []string
	if s.pdnsMgr != nil {
		conf := s.pdnsMgr.LoadConfig()
		if conf.VPSPublicIP != "" {
			hosts = append(hosts, conf.VPSPublicIP)
		}
		if nb := s.pdnsMgr.GetLocalNetbirdIP(); nb != "" {
			hosts = append(hosts, nb)
		}
	}
	return hosts
}

func (s *Server) isDirectPanelTLSActive() bool {
	if s.cfg != nil && strings.TrimSpace(s.cfg.TLSCert) != "" && strings.TrimSpace(s.cfg.TLSKey) != "" {
		return true
	}
	if s.authMgr == nil || !s.authMgr.IsPanelSSLEnabled() {
		return false
	}
	certPath, keyPath := s.authMgr.PanelSSLPaths()
	if _, err := os.Stat(certPath); err != nil {
		return false
	}
	if _, err := os.Stat(keyPath); err != nil {
		return false
	}
	return true
}

func (s *Server) wrapPlainPortHandler(mainHandler http.Handler) http.Handler {
	redirectInner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Izinkan upstream Nginx terpercaya pada mesin lokal (X-Forwarded-Proto: https) tetap lewat
		if !s.isDirectPanelTLSActive() || auth.IsHTTPS(r) {
			mainHandler.ServeHTTP(w, r)
			return
		}
		targetHost := strings.TrimSpace(r.Host)
		if targetHost == "" {
			targetHost = fmt.Sprintf("127.0.0.1:%d", s.panelPortNumber())
		}
		http.Redirect(w, r, "https://"+targetHost+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
	return s.wrapSecurityHeaders(s.wrapPanelDomain(redirectInner))
}

func (s *Server) listenAndServeMux(ln net.Listener, mainHandler http.Handler) error {
	httpChLn := newChanListener(ln.Addr())
	httpsChLn := newChanListener(ln.Addr())
	defer func() {
		_ = httpChLn.Close()
		_ = httpsChLn.Close()
	}()

	certCache := &tlsCertCache{}
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			certPath, keyPath := s.authMgr.PanelSSLPaths()
			if cert, err := certCache.getCertificate(certPath, keyPath); err == nil {
				return cert, nil
			}
			// Fallback regenerasi bila file sertifikat terhapus saat Panel SSL aktif
			if s.authMgr.IsPanelSSLEnabled() {
				if cp, kp, genErr := s.authMgr.EnsurePanelSelfSignedCert(s.panelExtraSANHosts()...); genErr == nil {
					return certCache.getCertificate(cp, kp)
				}
			}
			return nil, errors.New("sertifikat TLS panel tidak tersedia")
		},
	}

	httpSrv := &http.Server{
		Addr:              ln.Addr().String(),
		Handler:           s.wrapPlainPortHandler(mainHandler),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	httpsSrv := &http.Server{
		Addr:              ln.Addr().String(),
		Handler:           mainHandler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- httpSrv.Serve(httpChLn)
	}()
	go func() {
		tlsLn := tls.NewListener(httpsChLn, tlsCfg)
		errCh <- httpsSrv.Serve(tlsLn)
	}()

	go func() {
		for {
			rawConn, err := ln.Accept()
			if err != nil {
				errCh <- err
				return
			}
			go func(c net.Conn) {
				br := bufio.NewReaderSize(c, 1024)
				_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
				first, peekErr := br.Peek(1)
				_ = c.SetReadDeadline(time.Time{})
				if peekErr != nil || len(first) == 0 {
					if peekErr != io.EOF {
						_ = peekErr
					}
					_ = c.Close()
					return
				}
				pc := &peekConn{Conn: c, r: br}
				if s.isDirectPanelTLSActive() && first[0] == 0x16 {
					httpsChLn.enqueue(pc)
					return
				}
				httpChLn.enqueue(pc)
			}(rawConn)
		}
	}()

	return <-errCh
}
