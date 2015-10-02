package autoproxy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var (
	ErrCacheMiss     = errors.New("Cache miss")
	ErrCacheDisabled = errors.New("Cache disabled")
)

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

func safeParseUrl(in string) (*url.URL, error) {
	if !strings.HasPrefix(in, "http") {
		in = fmt.Sprintf("http://%s", in)
	}
	return url.Parse(in)
}

func parseProxyOverride(proxyOverride string) (overrides []*url.URL, localOverride bool) {
	overrides = make([]*url.URL, 0)
	for _, urlMaybe := range strings.Split(strings.TrimSpace(proxyOverride), ";") {
		urlMaybe = strings.TrimSpace(urlMaybe)
		if urlMaybe == "<local>" {
			localOverride = true
		} else {
			overrideUrl, err := safeParseUrl(urlMaybe)
			if err == nil {
				overrides = append(overrides, overrideUrl)
			}
		}
	}
	return
}
