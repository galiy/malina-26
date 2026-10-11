package masternode

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func hostPort(t *testing.T, raw string) (string, int) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	h, p, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	pi, _ := strconv.Atoi(p)
	return h, pi
}

func TestProxyCacheAndTTL(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `{"ok":1}`)
	}))
	defer ts.Close()
	host, port := hostPort(t, ts.URL)

	p := NewProxy(t.TempDir(), 60*time.Millisecond)
	n := Node{ID: "aabbccdd", Scheme: "http", Host: host, Port: port}

	r1 := p.Fetch(context.Background(), n, EpCheckDaemons, "/check_daemons.php")
	if r1.Status != 200 || r1.Body != `{"ok":1}` {
		t.Fatalf("r1=%+v", r1)
	}
	// Второй запрос — из кэша, без обращения к узлу.
	_ = p.Fetch(context.Background(), n, EpCheckDaemons, "/check_daemons.php")
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("кэш не сработал: hits=%d", got)
	}
	// Протухание — снова идём в узел.
	time.Sleep(80 * time.Millisecond)
	_ = p.Fetch(context.Background(), n, EpCheckDaemons, "/check_daemons.php")
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("после TTL ожидали 2 обращения, got=%d", got)
	}
}

func TestProxyRejectsBadID(t *testing.T) {
	p := NewProxy(t.TempDir(), time.Second)
	n := Node{ID: "../../pwn", Scheme: "http", Host: "127.0.0.1", Port: 1}
	r := p.Fetch(context.Background(), n, EpCheckDaemons, "/check_daemons.php")
	if r.Status != http.StatusBadGateway {
		t.Fatalf("плохой id должен давать no_data, got=%+v", r)
	}
}

func TestProxyNoData(t *testing.T) {
	p := NewProxy(t.TempDir(), time.Second)
	// 127.0.0.1:1 — почти наверняка закрыт → connection refused → no_data.
	n := Node{ID: "deadbeef", Scheme: "http", Host: "127.0.0.1", Port: 1}
	r := p.Fetch(context.Background(), n, EpCheckDaemons, "/check_daemons.php")
	if r.Status != http.StatusBadGateway || r.Body != `{"error":"no_data"}` {
		t.Fatalf("ожидали no_data, got=%+v", r)
	}
}

func TestProxyInvalidate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "X")
	}))
	defer ts.Close()
	host, port := hostPort(t, ts.URL)
	p := NewProxy(t.TempDir(), time.Hour)
	n := Node{ID: "11223344", Scheme: "http", Host: host, Port: port}
	_ = p.Fetch(context.Background(), n, EpMap, "/read_json.php?device=map")
	if _, ok := p.readCache(n.ID, EpMap); !ok {
		t.Fatal("кэш должен быть создан")
	}
	p.Invalidate(n.ID)
	if _, ok := p.readCache(n.ID, EpMap); ok {
		t.Fatal("после Invalidate кэш должен исчезнуть")
	}
}

func TestDeviceEndpoint(t *testing.T) {
	if _, ok := DeviceEndpoint("map"); !ok {
		t.Fatal("map")
	}
	if _, ok := DeviceEndpoint("bat"); !ok {
		t.Fatal("bat")
	}
	if _, ok := DeviceEndpoint("mppt"); !ok {
		t.Fatal("mppt")
	}
	if _, ok := DeviceEndpoint("kes"); ok {
		t.Fatal("kes не должен приниматься")
	}
}
