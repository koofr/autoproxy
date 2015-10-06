//+build windows
package autoproxy

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"golang.org/x/sys/windows/registry"
	"github.com/robertkrimen/otto"
)

var (
	initOnce                sync.Once
	cache                   map[string]*url.URL
	cacheLock               sync.RWMutex
	useLookup               bool
	regNotifyChangeKeyValue *syscall.Proc
	notifyAddrChange        *syscall.Proc
	notifyCh                chan error
	listeners               map[string]chan *url.URL
)

const (
	PathInternetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

// SmartProxy first does lookup if it already knows proxy server for given URL and returns cached result, otherwise it fallback to Proxy() function. In case it does not manage to initailize cache, it returns ErrCacheDisabled error and one should use Proxy() function in this case
func SmartProxy(req *http.Request) (proxyUrl *url.URL, err error) {

	initOnce.Do(initCache)

	if !useLookup {
		err = ErrCacheDisabled
		return
	}

	proxyUrl, err = lookup(req)
	if err != nil {
		proxyUrl, err = Proxy(req)
		if err == nil {
			setCache(req, proxyUrl)
		}
	}

	return
}

// Proxy returns system proxy
func Proxy(req *http.Request) (proxyUrl *url.URL, err error) {
	proxyUrl, err = ProxyFromAutoConfig(req)

	if proxyUrl == nil || err != nil {
		proxyUrl, err = ProxyFromRegistry(req)
	}
	return
}

func setCache(req *http.Request, proxyUrl *url.URL) {
	cacheLock.Lock()
	defer cacheLock.Unlock()

	addr := canonicalAddr(req.URL)
	cache[addr] = proxyUrl
}

func lookup(req *http.Request) (proxyUrl *url.URL, err error) {
	cacheLock.RLock()
	defer cacheLock.RUnlock()

	addr := canonicalAddr(req.URL)

	proxyUrl, has := cache[addr]
	if !has {
		return nil, ErrCacheMiss
	}

	return proxyUrl, nil
}

func initCache() {
	if iphlpapi, err := syscall.LoadDLL("Iphlpapi.dll"); err == nil {
		if p, err := iphlpapi.FindProc("NotifyAddrChange"); err == nil {
			notifyAddrChange = p
		}
	}

	if advapi32, err := syscall.LoadDLL("Advapi32.dll"); err == nil {
		if p, err := advapi32.FindProc("RegNotifyChangeKeyValue"); err == nil {
			regNotifyChangeKeyValue = p
		}
	}

	cacheLock.Lock()
	defer cacheLock.Unlock()

	notifyCh = make(chan error)

	if regNotifyChangeKeyValue != nil && notifyAddrChange != nil {
		useLookup = true
		go notifyIpChange(notifyCh)
		go notifyRegChange(registry.CURRENT_USER, PathInternetSettings, notifyCh)
	}

	cache = make(map[string]*url.URL)

	go func() {
		for _ = range notifyCh {
			invalidateCache()
		}
	}()
}

func invalidateCache() {
	cacheLock.Lock()
	defer cacheLock.Unlock()
	cache = make(map[string]*url.URL)
}

func notifyIpChange(notifyCh chan error) {

	for {
		notifyAddrChange.Call(0, 0)
		notifyCh <- nil
	}
}

func notifyRegChange(key registry.Key, path string, notifyCh chan error) (err error) {
	k, err := registry.OpenKey(key, path, syscall.KEY_NOTIFY)
	if err != nil {
		return
	}
	for {
		regNotifyChangeKeyValue.Call(uintptr(k), 0, 0x00000001|0x00000004, 0, 0)
		notifyCh <- nil
	}
}

func ProxyFromRegistry(req *http.Request) (proxyUrl *url.URL, err error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, PathInternetSettings, registry.READ)
	if err != nil {
		return
	}
	defer k.Close()

	proxyEnable, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil {
		return
	}

	if proxyEnable != 1 {
		return nil, nil
	}

	proxyServerStr, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return
	}

	proxyOverrideStr, _, err := k.GetStringValue("ProxyOverride")
	if err == nil {
		_, localOverride := parseProxyOverride(proxyOverrideStr)

		host, _, err := net.SplitHostPort(canonicalAddr(req.URL))
		if err != nil {
			return nil, err
		}

		// if proxy is disabled for local addreses
		if localOverride {
			if host == "localhost" {
				return nil, nil
			}
			if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
				return nil, nil
			}
		}

		// TODO check for custom overrides
	}

	proxyUrl, err = safeParseUrl(proxyServerStr)
	return
}

func ProxyFromAutoConfig(req *http.Request) (proxyUrl *url.URL, err error) {

	// check if AutoConfigURL is definded in registry, otherwise no point in spending time to check it
	k, err := registry.OpenKey(registry.CURRENT_USER, PathInternetSettings, registry.READ)
	if err != nil {
		return
	}
	defer k.Close()

	autoConfigURL, _, err := k.GetStringValue("AutoConfigURL")
	if err != nil {
		// no auto proxy url
		return nil, nil
	}

	res, err := http.Get(autoConfigURL)
	if err != nil {
		return nil, err
	}

	vm := otto.New()
	vm.Run(res.Body)
	vm.Set("url", req.URL)
	vm.Set("host", req.URL.Host)
	vm.Run("proxy = FindProxyForURL(url, host);")
	val, err := vm.Get("proxy")
	if err != nil {
		return nil, err
	}

	proxyStr, err := val.ToString()
	if err != nil {
		return nil, err
	}
	// parse it
	// format reference: https://en.wikipedia.org/wiki/Proxy_auto-config
	for _, proxyMaybe := range strings.Split(proxyStr, ";") {
		proxyMaybe = strings.ToLower(proxyMaybe)
		if strings.Contains(proxyMaybe, "direct") {
			return nil, nil
		}

		proxyMaybe = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(proxyMaybe), "proxy "))

		urlMaybe, err := safeParseUrl(proxyMaybe)
		if err == nil {
			return urlMaybe, err
		}
	}

	return
}