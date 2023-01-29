package loadbalancer

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"
)

const (
	Attempts int = iota
	Retry
)

type HttpLoadBalancer struct {
	Endpoints []*Backend
	current   uint64
}

var httpLB HttpLoadBalancer

func InitHttpLoadBalancer() {
	httpLB = HttpLoadBalancer{}

	go httpLB.HealthCheck()
}

func AddEndpoint(newUrl string) error {
	endpointUrl, err := url.Parse(newUrl)
	if err != nil {
		return err
	}

	proxy := httputil.NewSingleHostReverseProxy(endpointUrl)

	backend := &Backend{
		URL:          endpointUrl,
		ReverseProxy: *proxy,
	}

	backend.ReverseProxy.ErrorHandler = func(res http.ResponseWriter, req *http.Request, err error) {
		log.Printf("[%s] %s\n", "serverUrl.Host", err.Error())
		retries := getRetryFromContext(req)
		if retries < 3 {
			select {
			case <-time.After(10 * time.Millisecond):
				ctx := context.WithValue(req.Context(), Retry, retries)
				proxy.ServeHTTP(res, req.WithContext(ctx))
			}
			return
		}

		MarkEndpointStatus(*endpointUrl, false)

		attempts := getAttemptsFromContext(req)
		log.Printf("%s(%s) Attempting retry %d\n", req.RemoteAddr, req.URL.Path, attempts)
		ctx := context.WithValue(req.Context(), Attempts, attempts+1)
		LoadBalance(req.WithContext(ctx), res)

	}

	httpLB.Endpoints = append(httpLB.Endpoints, backend)

	return nil
}

func MarkEndpointStatus(endpointUrl url.URL, status bool) {
	s := "up"
	if !status {
		s = "down"
	}
	log.Printf("%s [%s]\n", endpointUrl.String(), s)
}

func nextIdx() int {
	return int(atomic.AddUint64(&httpLB.current, uint64(1)) % uint64(len(httpLB.Endpoints)))
}

func getNextPeer() *Backend {
	next := nextIdx()
	for i := next; i <= len(httpLB.Endpoints); i++ {
		idx := i % len(httpLB.Endpoints)
		if httpLB.Endpoints[idx].IsAlive() {
			if i != next {
				atomic.StoreUint64(&httpLB.current, uint64(idx))
			}
			return httpLB.Endpoints[idx]
		}
	}
	return nil
}

func getRetryFromContext(req *http.Request) int {
	if retry, ok := req.Context().Value(Retry).(int); ok {
		return retry
	}
	return 0
}

func getAttemptsFromContext(req *http.Request) int {
	if attempts, ok := req.Context().Value(Attempts).(int); ok {
		return attempts
	}
	return 0
}

func LoadBalance(req *http.Request, res http.ResponseWriter) {
	attempts := getAttemptsFromContext(req)
	if attempts > 3 {
		log.Printf("%s(%s) Max attempts reached, terminating\n", req.RemoteAddr, req.URL.Path)
		http.Error(res, "Service not available", http.StatusServiceUnavailable)
		return

	}

	peer := getNextPeer()
	if peer != nil {
		peer.ReverseProxy.ServeHTTP(res, req)
		return
	}
	http.Error(res, "Service is not available", http.StatusServiceUnavailable)
}

func isBackendAlive(u *url.URL) bool {
	timeout := 2 * time.Second

	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		log.Println("Site unreachable, error: ", err)
		return false
	}

	_ = conn.Close()
	return true
}

func (lb *HttpLoadBalancer) HealthCheck() {
	for _, endpoint := range lb.Endpoints {
		status := "up"
		alive := isBackendAlive(endpoint.URL)
		endpoint.SetAlive(alive)
		if !alive {
			status = "down"
		}
		log.Printf("%s [%s]\n", endpoint.URL.String(), status)
	}
}

func healthCheck() {
	t := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-t.C:
			log.Println("Starting health check...")
			httpLB.HealthCheck()
			log.Println("Health check completed")
		}
	}
}
