package autoproxy

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var (
	ErrCacheMiss = errors.New("Cache miss")
)

func fixUrlSchema(u *url.URL) {
	if u.Scheme == "" {
		u.Scheme = "http"
	}
}

var portMap = map[string]string{
	"http":  "80",
	"https": "443",
}

func hasPort(addr string) bool {
	_, p, _ := net.SplitHostPort(addr)
	return p != ""
}

// canonicalAddr returns url.Host but always with a ":port" suffix
func canonicalAddr(url *url.URL) string {
	addr := url.Host
	if !hasPort(addr) {
		return addr + ":" + portMap[url.Scheme]
	}
	return addr
}

func parseProxyOverride(proxyOverride string) (overrides []*url.URL, localOverride bool) {
	overrides = make([]*url.URL, 0)
	for _, urlMaybe := range strings.Split(strings.TrimSpace(proxyOverride), ";") {
		urlMaybe = strings.TrimSpace(urlMaybe)
		if urlMaybe == "<local>" {
			localOverride = true
		} else {
			overrideUrl, err := url.Parse(urlMaybe)
			if err == nil {
				fixUrlSchema(overrideUrl)
				overrides = append(overrides, overrideUrl)
			}
		}
	}
	return
}
