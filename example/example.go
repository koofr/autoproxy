package main

import (
	"fmt"
	"github.com/koofr/autoproxy"
	"net/http"
	"time"
)

func main() {

	fakeReq, _ := http.NewRequest("GET", "http://google.com", nil)

	var start time.Time

	for i := 1; i < 11; i++ {
		fmt.Println("======== Attemp", i, "=================")
		start = time.Now()
		p, err := autoproxy.ProxyFromRegistry(fakeReq)
		d := time.Since(start)
		fmt.Printf("ProxyyFromRegistry: %s %v took %s\n", p, err, d)

		start = time.Now()
		p, err = autoproxy.ProxyFromAutoConfig(fakeReq)
		d = time.Since(start)
		fmt.Printf("ProxyFromAutoConfig: %s %v took %s\n", p, err, d)

		start = time.Now()
		p, err = autoproxy.Proxy(fakeReq)
		d = time.Since(start)
		fmt.Printf("Proxy %s %v took %s\n", p, err, d)
	}

	for i := 1; i < 11; i++ {
		fmt.Println("======== Attemp", i, "=================")
		start = time.Now()
		p, err := autoproxy.ProxyFromRegistry(fakeReq)
		d := time.Since(start)
		fmt.Printf("ProxyyFromRegistry: %s %v took %s\n", p, err, d)

		start = time.Now()
		p, err = autoproxy.ProxyFromAutoConfig(fakeReq)
		d = time.Since(start)
		fmt.Printf("ProxyFromAutoConfig: %s %v took %s\n", p, err, d)

		start = time.Now()
		p, err = autoproxy.SmartProxy(fakeReq)
		d = time.Since(start)
		fmt.Printf("SmartProxy %s %v took %s\n", p, err, d)
	}


	p, err := autoproxy.SmartProxy(fakeReq)
	if err != nil {
		fmt.Println("err", err)
	}
	for {
		time.Sleep(time.Second)
		newProxy, err := autoproxy.SmartProxy(fakeReq)
		if err != nil {
			fmt.Println("err", err)
			continue
		}
		if newProxy != p {
			p = newProxy
			fmt.Printf("SmartProxy %s\n", p)
		}

	}
}
