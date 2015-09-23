//+build linux
package autoproxy

import (
	"net/http"
	"net/url"
)

func SmartProxy(req *http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment(req)
}

func Proxy(req *http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment(req)
}
