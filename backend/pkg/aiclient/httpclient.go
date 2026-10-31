package aiclient

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewHTTPClient 构造带超时的 HTTP 客户端
//
// 关键点：默认的 http.ProxyFromEnvironment 会把本机 / 内网 IP 的请求也送进
// 系统代理（本地开发环境常见），导致自建网关返回 403。这里对
// localhost、127.x、私有网段和 IP 字面量一律直连，其余仍走环境代理。
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 proxyFor,
			MaxIdleConns:          20,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func proxyFor(req *http.Request) (*url.URL, error) {
	host := req.URL.Hostname()

	// 本机与内网直连
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return nil, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return nil, nil
		}
		// 其余 IP 字面量（公网自建网关）也直连，避免代理按域名白名单拦截
		return nil, nil
	}

	return http.ProxyFromEnvironment(req)
}
