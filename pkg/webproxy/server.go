package webproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

const shutdownTimeout = 5 * time.Second

type Server struct {
	server      *http.Server
	transport   *http.Transport
	auth        proxyAuth
	recordings  *recordingManager
	credentials *credentialManager
	coreService webProxyService
}

func NewServer(bindHost, port, recordingRoot, ffmpegPath string, coreService webProxyService) (*Server, error) {

	proxy := &Server{
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
		auth:        proxyAuth{sessions: make(map[string]*proxySession)},
		credentials: newCredentialManager(coreService),
		coreService: coreService,
	}
	if recordingRoot != "" {
		manager, err := newRecordingManager(recordingRoot, ffmpegPath)
		if err != nil {
			return nil, err
		}
		proxy.recordings = manager
	}
	proxy.server = &http.Server{
		Addr:              net.JoinHostPort(bindHost, port),
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	return proxy, nil
}

func (s *Server) Start() {
	logger.Infof("Start Web proxy server at %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Errorf("Web proxy server stopped unexpectedly: %s", err)
	}
}

func (s *Server) Stop() {
	s.stopProxySessions()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		logger.Errorf("Stop Web proxy server failed: %s", err)
	}
	s.transport.CloseIdleConnections()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, credentialPathPrefix) && !r.URL.IsAbs() {
		s.serveCredentials(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, recordingPathPrefix) && !r.URL.IsAbs() {
		s.serveRecording(w, r)
		return
	}

	auth := s.authenticateProxy(r)
	if auth == nil || auth.locked {
		requireProxyAuth(w)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(auth.ctx, cancel)
	defer stop()
	r = r.WithContext(ctx)

	target := r.Host
	if r.Method != http.MethodConnect {
		target = r.URL.Host
	}
	address, ok := auth.allowTarget(target)
	if !ok {
		http.Error(w, "target not allowed for this asset", http.StatusForbidden)
		logger.Warnf("Web proxy session %s: blocked target %s", auth.id, target)
		return
	}
	if r.Method == http.MethodConnect {
		s.serveTunnel(w, r, address)
		return
	}
	s.serveHTTP(w, r)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
		http.Error(w, "unsupported target scheme", http.StatusBadRequest)
		return
	}

	outbound := r.Clone(r.Context())
	outbound.RequestURI = ""
	removeHopByHopHeaders(outbound.Header)

	response, err := s.transport.RoundTrip(outbound)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		logger.Warnf("Web proxy request to %s failed: %s", r.URL.Host, err)
		return
	}
	defer response.Body.Close()

	removeHopByHopHeaders(response.Header)
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
	logger.Infof("Web proxy %s %s -> %d", r.Method, r.URL.Host, response.StatusCode)
}

func (s *Server) serveTunnel(w http.ResponseWriter, r *http.Request, address string) {
	// the address that was checked (an IP), never the name again: no DNS rebinding in between
	upstream, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(r.Context(), "tcp", address)
	if err != nil {
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
		logger.Warnf("Web proxy CONNECT to %s failed: %s", r.Host, err)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "connection hijacking is unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	defer client.Close()
	defer upstream.Close()
	stop := context.AfterFunc(r.Context(), func() {
		client.Close()
		upstream.Close()
	})
	defer stop()

	if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffered.Flush(); err != nil {
		return
	}

	done := make(chan struct{}, 2)
	go copyTunnel(upstream, buffered, done)
	go copyTunnel(client, upstream, done)
	<-done
	logger.Infof("Web proxy CONNECT %s closed", r.Host)
}

func removeHopByHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection",
		"TE", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(name)
	}
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func copyTunnel(dst io.Writer, src io.Reader, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	done <- struct{}{}
}

// XPAM: what a proxy session may reach. Public internet hosts stay reachable (web pages load files
// from CDNs); private, loopback, link-local and other internal addresses only when they are the
// asset itself or one of its allowed sites, so the proxy is no way into XPAM's own containers
// (redis, postgresql, core...) or other internal servers the user has no permission for.

func allowedTargets(origin string, allowedURLs []string) []string {
	hosts := []string{}
	if parsed, err := url.Parse(origin); err == nil && parsed.Hostname() != "" {
		hosts = append(hosts, strings.ToLower(parsed.Hostname()))
	}
	for _, item := range allowedURLs {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		wildcard := strings.HasPrefix(item, "*.")
		raw := strings.TrimPrefix(item, "*.")
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		host := parsed.Hostname()
		if wildcard {
			host = "*." + host
		}
		hosts = append(hosts, host)
	}
	return hosts
}

func (p *proxySession) hostListed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, allowed := range p.allowed {
		if strings.HasPrefix(allowed, "*.") {
			base := allowed[2:]
			if host == base || strings.HasSuffix(host, "."+base) {
				return true
			}
		} else if host == allowed {
			return true
		}
	}
	return false
}

func internalAddress(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) // carrier-grade NAT 100.64/10
}

// allowTarget returns the address to connect to ("ip:port") when the session may reach target.
func (p *proxySession) allowTarget(target string) (string, bool) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		host, port = target, "443"
	}
	if host == "" {
		return "", false
	}
	listed := p.hostListed(host)
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "", false
	}
	for _, ip := range ips {
		if internalAddress(ip) && !listed {
			return "", false
		}
	}
	return net.JoinHostPort(ips[0].String(), port), true
}
