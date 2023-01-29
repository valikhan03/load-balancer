package loadbalancer

import (
	"net/http/httputil"
	"net/url"
	"sync"
)

type Backend struct {
	URL          *url.URL
	Alive        bool
	ReverseProxy httputil.ReverseProxy
	mux          sync.RWMutex
}

func (b *Backend) SetAlive(status bool) {
	b.mux.Lock()
	b.Alive = status
	b.mux.Unlock()
}

func (b *Backend) IsAlive() (status bool) {
	b.mux.RLock()
	status = b.Alive
	b.mux.RUnlock()
	return
}
