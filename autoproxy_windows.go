//+build windows
package autoproxy

import (
	"golang.org/x/sys/windows/registry"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"unsafe"
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

	wininet = syscall.MustLoadDLL("wininet.dll")
	jsproxy = syscall.MustLoadDLL("jsproxy.dll")

	internetGetProxyInfo = jsproxy.MustFindProc("InternetGetProxyInfo")
	internetOpen         = wininet.MustFindProc("InternetOpenW")
	internetCloseHandle  = wininet.MustFindProc("InternetCloseHandle")
	internetOpenUrl      = wininet.MustFindProc("InternetOpenUrlW")
)

const (
	PathInternetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

func SmartProxy(req *http.Request) (proxyUrl *url.URL, err error) {

	initOnce.Do(initCache)

	if useLookup {
		proxyUrl, err = lookup(req)
	}

	if err != nil || useLookup == false {
		proxyUrl, err = Proxy(req)
	}

	return
}

func Proxy(req *http.Request) (proxyUrl *url.URL, err error) {
	proxyUrl, err = ProxyFromAutoConfig(req)

	if proxyUrl == nil || err != nil {
		proxyUrl, err = ProxyFromRegistry(req)
	}
	return
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
		if notifyAddrChange, err := iphlpapi.FindProc("NotifyAddrChange"); err == nil {
			notifyAddrChange = notifyAddrChange
		}
	}

	if advapi32, err := syscall.LoadDLL("Advapi32.dll"); err == nil {
		if regNotifyChangeKeyValue, err := advapi32.FindProc("RegNotifyChangeKeyValue"); err == nil {
			regNotifyChangeKeyValue = regNotifyChangeKeyValue
		}
	}

	cacheLock.Lock()
	defer cacheLock.Unlock()

	if regNotifyChangeKeyValue != nil && notifyAddrChange != nil {
		useLookup = true
		go notifyIpChange(notifyCh)
		go notifyRegChange(registry.CURRENT_USER, PathInternetSettings, notifyCh)
	}

	notifyCh = make(chan error)
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

	_, _, err = k.GetStringValue("AutoConfigURL")
	if err != nil {
		// no auto proxy url
		return nil, nil
	}

	// just do fake request to make windows load proxy settings, does not work otherwise
	hInternet, _, _ := internetOpen.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("fake req"))), 0, 0, 0, 0)
	internetOpenUrl.Call(hInternet, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("http://0.0.0.0"))), 0, 0, 0, 0)
	internetCloseHandle.Call(hInternet)

	lpszUrl := syscall.StringBytePtr(req.URL.String())
	dwUrlLength := uint32(len(req.URL.String()))

	lpszUrlHostName := syscall.StringBytePtr(req.URL.Host)
	dwUrlHostNameLength := uint32(len(req.URL.Host))

	lplpszProxyHostName := make([]byte, 1024, 1024)
	lpdwProxyHostNameLength := uint32(len(lplpszProxyHostName))

	r1, _, err := internetGetProxyInfo.Call(
		uintptr(unsafe.Pointer(lpszUrl)),
		uintptr(dwUrlLength),
		uintptr(unsafe.Pointer(lpszUrlHostName)),
		uintptr(dwUrlHostNameLength),
		uintptr(unsafe.Pointer(&lplpszProxyHostName)),
		uintptr(unsafe.Pointer(&lpdwProxyHostNameLength)),
	)

	// if syscall returns false
	if r1 != 1 {
		return
	}
	// clear error success stupidity
	err = nil

	// create string from buffer
	proxyStr := string(lplpszProxyHostName[0 : lpdwProxyHostNameLength-1])

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
