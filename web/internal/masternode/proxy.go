package masternode

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Endpoint — ключ ручки (используется в имени файла кэша).
type Endpoint string

const (
	EpCheckDaemons Endpoint = "check_daemons"
	EpMap          Endpoint = "read_json_map"
	EpBat          Endpoint = "read_json_bat"
	EpMPPT         Endpoint = "read_json_mppt"
)

// DeviceEndpoint — endpoint кэша для device=… read_json.
func DeviceEndpoint(device string) (Endpoint, bool) {
	switch device {
	case "map":
		return EpMap, true
	case "bat":
		return EpBat, true
	case "mppt":
		return EpMPPT, true
	}
	return "", false
}

// CheckDaemonsPath, ReadJSONPath — пути legacy-ручек на узле.
func CheckDaemonsPath() string          { return "/check_daemons.php" }
func ReadJSONPath(device string) string { return "/read_json.php?device=" + device }

// Response — сохранённый/отдаваемый ответ (тело как есть).
type Response struct {
	Status int    `json:"status"`
	CType  string `json:"ctype"`
	Body   string `json:"body"`
}

// Proxy — ленивый кэширующий прокси: по запросу читает/пишет кэш в tmpfs,
// иначе идёт в узел (таймаут 1 c, https без проверки серта). Кэширует и неуспех.
type Proxy struct {
	dir    string
	ttl    time.Duration
	client *http.Client

	mu     sync.Mutex
	flight map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	resp Response
}

// NewProxy создаёт прокси; ttl<=0 → 5 c.
func NewProxy(dir string, ttl time.Duration) *Proxy {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &Proxy{
		dir: dir,
		ttl: ttl,
		client: &http.Client{
			Timeout: time.Second,
			Transport: &http.Transport{
				// Осознанное решение (docs/master-node.md): серт не проверяем.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			// Редиректы не ходим — отдаём ответ как есть (как legacy).
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		flight: map[string]*flightCall{},
	}
}

func noData() Response {
	return Response{Status: http.StatusBadGateway, CType: "application/json", Body: `{"error":"no_data"}`}
}

func (p *Proxy) path(id string, ep Endpoint) string {
	return filepath.Join(p.dir, id+"."+string(ep)+".json")
}

func (p *Proxy) readCache(id string, ep Endpoint) (Response, bool) {
	if !ValidID(id) {
		return Response{}, false
	}
	// Один fd: Stat и чтение по одному открытому файлу (без гонки Stat/Open).
	f, err := os.Open(p.path(id, ep))
	if err != nil {
		return Response{}, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || time.Since(fi.ModTime()) > p.ttl {
		return Response{}, false
	}
	b, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return Response{}, false
	}
	var r Response
	if json.Unmarshal(b, &r) != nil {
		return Response{}, false
	}
	return r, true
}

func (p *Proxy) writeCache(id string, ep Endpoint, r Response) {
	if !ValidID(id) {
		return
	}
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		log.Printf("master: cache dir %s: %v", p.dir, err)
		return
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	fp := p.path(id, ep)
	tmp := fp + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("master: cache write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, fp); err != nil {
		log.Printf("master: cache rename %s: %v", fp, err)
		_ = os.Remove(tmp)
	}
}

// Invalidate — сбросить кэш узла (при изменении/удалении).
func (p *Proxy) Invalidate(id string) {
	if !ValidID(id) {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(p.dir, id+".*.json"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// Fetch отдаёт ответ по (узел, endpoint), обновляя кэш по TTL.
// pathQuery — legacy-путь с query, напр. "/read_json.php?device=map".
func (p *Proxy) Fetch(ctx context.Context, n Node, ep Endpoint, pathQuery string) Response {
	if !ValidID(n.ID) {
		return noData()
	}
	key := n.ID + "|" + string(ep)

	if r, ok := p.readCache(n.ID, ep); ok {
		return r
	}

	p.mu.Lock()
	if f, ok := p.flight[key]; ok {
		p.mu.Unlock()
		select {
		case <-f.done:
			return f.resp
		case <-ctx.Done():
			return noData()
		}
	}
	f := &flightCall{done: make(chan struct{})}
	p.flight[key] = f
	p.mu.Unlock()

	// Перечитать кэш: возможно, обновил его тот, кого ждали бы.
	if r, ok := p.readCache(n.ID, ep); ok {
		p.mu.Lock()
		delete(p.flight, key)
		p.mu.Unlock()
		close(f.done)
		return r
	}

	r := p.fetchRemote(ctx, n, pathQuery)
	p.writeCache(n.ID, ep, r)

	p.mu.Lock()
	delete(p.flight, key)
	p.mu.Unlock()
	f.resp = r
	close(f.done)
	return r
}

func (p *Proxy) fetchRemote(ctx context.Context, n Node, pathQuery string) Response {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.BaseURL()+pathQuery, nil)
	if err != nil {
		return noData()
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return noData()
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return noData()
	}
	ct := resp.Header.Get("Content-Type")
	if strings.TrimSpace(ct) == "" {
		ct = "text/html; charset=UTF-8"
	}
	return Response{Status: resp.StatusCode, CType: ct, Body: string(body)}
}
