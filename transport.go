package crema

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// ChromeTransport creates an http.RoundTripper that mimics Chrome's TLS fingerprint.
func ChromeTransport() http.RoundTripper {
	return &chromeRoundTripper{}
}

// ProxyTransport creates a Chrome-fingerprinted transport that routes through a proxy.
func ProxyTransport(proxyURL string) http.RoundTripper {
	return &chromeRoundTripper{proxyURL: proxyURL}
}

type chromeRoundTripper struct {
	proxyURL string
}

func (t *chromeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return http.DefaultTransport.RoundTrip(req)
	}

	addr := req.URL.Host
	if !hasPort(addr) {
		addr += ":443"
	}

	host, _, _ := net.SplitHostPort(addr)
	if host == "" {
		host = req.URL.Hostname()
	}

	// Dial with timeout
	var rawConn net.Conn
	var err error

	if t.proxyURL != "" {
		rawConn, err = dialProxy(t.proxyURL, addr)
	} else {
		rawConn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		return nil, err
	}

	// TLS handshake with Chrome fingerprint
	rawConn.SetDeadline(time.Now().Add(10 * time.Second))
	tlsConn := utls.UClient(rawConn, &utls.Config{
		ServerName: host,
	}, utls.HelloChrome_131)

	if err := tlsConn.Handshake(); err != nil {
		rawConn.Close()
		return nil, err
	}

	// Set deadline for the entire HTTP exchange
	rawConn.SetDeadline(time.Now().Add(15 * time.Second))

	alpn := tlsConn.ConnectionState().NegotiatedProtocol

	var resp *http.Response

	if alpn == "h2" {
		// HTTP/2: use http2.Transport with a hard timeout on the entire exchange
		h2t := &http2.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return tlsConn, nil
			},
		}

		type h2Result struct {
			resp *http.Response
			body []byte
			err  error
		}
		ch := make(chan h2Result, 1)
		go func() {
			r, e := h2t.RoundTrip(req)
			if e != nil {
				ch <- h2Result{err: e}
				return
			}
			b, re := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
			r.Body.Close()
			if re != nil {
				ch <- h2Result{err: re}
				return
			}
			ch <- h2Result{resp: r, body: b}
		}()

		select {
		case res := <-ch:
			h2t.CloseIdleConnections()
			rawConn.Close()
			if res.err != nil {
				return nil, res.err
			}
			res.resp.Body = io.NopCloser(io.NewSectionReader(bytesReaderAt(res.body), 0, int64(len(res.body))))
			return res.resp, nil
		case <-time.After(5 * time.Second):
			// Force-close everything to unblock the stuck goroutine
			tlsConn.Close()
			rawConn.Close()
			h2t.CloseIdleConnections()
			return nil, fmt.Errorf("h2 request timeout")
		}
	}

	// HTTP/1.1: straightforward one-shot transport
	h1t := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return tlsConn, nil
		},
		DisableKeepAlives: true,
	}
	resp, err = h1t.RoundTrip(req)
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	// Read full body and close
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	resp.Body.Close()
	h1t.CloseIdleConnections()
	rawConn.Close()
	if readErr != nil {
		return nil, readErr
	}
	resp.Body = io.NopCloser(io.NewSectionReader(bytesReaderAt(bodyBytes), 0, int64(len(bodyBytes))))
	return resp, nil
}

// bytesReaderAt wraps []byte to implement io.ReaderAt
type bytesReaderAt []byte

func (b bytesReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	n = copy(p, b[off:])
	if n < len(p) {
		err = io.EOF
	}
	return
}

// ─── Proxy support ──────────────────────────────────────────

func dialProxy(proxyURL, targetAddr string) (net.Conn, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	proxyAddr := u.Host
	if !hasPort(proxyAddr) {
		if u.Scheme == "socks5" {
			proxyAddr += ":1080"
		} else {
			proxyAddr += ":8080"
		}
	}

	if u.Scheme == "socks5" {
		return dialSOCKS5(proxyAddr, u.User, targetAddr)
	}

	conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to proxy: %w", err)
	}

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", targetAddr, targetAddr)
	if u.User != nil {
		user := u.User.Username()
		pass, _ := u.User.Password()
		auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		connectReq += "Proxy-Authorization: Basic " + auth + "\r\n"
	}
	connectReq += "\r\n"

	if _, err = conn.Write([]byte(connectReq)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("sending CONNECT: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reading CONNECT response: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("proxy CONNECT returned %d", resp.StatusCode)
	}

	conn.SetDeadline(time.Time{})
	return conn, nil
}

func dialSOCKS5(proxyAddr string, userInfo *url.Userinfo, targetAddr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to SOCKS5 proxy: %w", err)
	}

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	hasAuth := userInfo != nil && userInfo.Username() != ""
	if hasAuth {
		conn.Write([]byte{0x05, 0x02, 0x00, 0x02})
	} else {
		conn.Write([]byte{0x05, 0x01, 0x00})
	}

	buf := make([]byte, 2)
	if _, err := conn.Read(buf); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 greeting: %w", err)
	}

	if buf[1] == 0x02 && hasAuth {
		user := userInfo.Username()
		pass, _ := userInfo.Password()
		authReq := []byte{0x01, byte(len(user))}
		authReq = append(authReq, []byte(user)...)
		authReq = append(authReq, byte(len(pass)))
		authReq = append(authReq, []byte(pass)...)
		conn.Write(authReq)

		authResp := make([]byte, 2)
		if _, err := conn.Read(authResp); err != nil || authResp[1] != 0x00 {
			conn.Close()
			return nil, fmt.Errorf("SOCKS5 auth failed")
		}
	} else if buf[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 no acceptable auth method")
	}

	host, portStr, _ := net.SplitHostPort(targetAddr)
	port := 443
	fmt.Sscanf(portStr, "%d", &port)

	connectReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connectReq = append(connectReq, []byte(host)...)
	connectReq = append(connectReq, byte(port>>8), byte(port&0xff))
	conn.Write(connectReq)

	resp := make([]byte, 10)
	if _, err := conn.Read(resp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect: %w", err)
	}

	if resp[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect failed: code %d", resp[1])
	}

	conn.SetDeadline(time.Time{})
	return conn, nil
}

func hasPort(addr string) bool {
	_, _, err := net.SplitHostPort(addr)
	return err == nil
}
