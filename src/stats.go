package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type usageB struct {
	T  int64  `json:"t"`
	Rx uint64 `json:"rx"`
	Tx uint64 `json:"tx"`
}
type latP struct {
	T  int64 `json:"t"`
	Ms int   `json:"ms"`
}

var (
	mu       sync.Mutex
	rateRx   float64
	rateTx   float64
	speed    [60][2]float64 // per-minit average, ring
	live     [60][2]float64 // sampel 3 saat, ring (3 minit)
	speedT   int64
	usage    []usageB
	lat      []latP
	ipCache  map[string]any
	ipTime   time.Time
	lastRx   uint64
	lastTx   uint64
	haveLast bool
	accRx    float64
	accTx    float64
	accN     int
	dirty    bool
)

func wanIface() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) > 3 && f[1] == "00000000" {
			return f[0]
		}
	}
	return ""
}

func netBytes(ifn string) (rx, tx uint64, ok bool) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, ifn+":") {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(l, ifn+":"))
		if len(f) < 9 {
			return
		}
		rx, _ = strconv.ParseUint(f[0], 10, 64)
		tx, _ = strconv.ParseUint(f[8], 10, 64)
		return rx, tx, true
	}
	return
}

func loadUsage() {
	if b, err := os.ReadFile(home + "/etc/usage.json"); err == nil {
		_ = json.Unmarshal(b, &usage)
	}
}

func saveUsage() {
	mu.Lock()
	b, _ := json.Marshal(usage)
	dirty = false
	mu.Unlock()
	_ = os.WriteFile(home+"/etc/usage.json", b, 0600)
}

func sampler() {
	loadUsage()
	tk := time.NewTicker(3 * time.Second)
	lastSave := time.Now()
	for now := range tk.C {
		ifn := wanIface()
		trackWan(ifn != "" && ifaceIP(ifn) != "")
		rx, tx, ok := netBytes(ifn)
		if !ok {
			continue
		}
		mu.Lock()
		if haveLast && rx >= lastRx && tx >= lastTx {
			dr, dt := rx-lastRx, tx-lastTx
			rateRx, rateTx = float64(dr)/3, float64(dt)/3
			copy(live[0:], live[1:])
			live[59] = [2]float64{rateRx, rateTx}
			accRx += rateRx
			accTx += rateTx
			accN++
			hr := now.Unix() / 3600 * 3600
			if len(usage) == 0 || usage[len(usage)-1].T != hr {
				usage = append(usage, usageB{T: hr})
				if len(usage) > 720 {
					usage = usage[len(usage)-720:]
				}
			}
			usage[len(usage)-1].Rx += dr
			usage[len(usage)-1].Tx += dt
			dirty = true
			mi := now.Unix() / 60
			if speedT == 0 {
				speedT = mi
			}
			for speedT < mi {
				n := float64(accN)
				if n == 0 {
					n = 1
				}
				copy(speed[0:], speed[1:])
				speed[59] = [2]float64{accRx / n, accTx / n}
				accRx, accTx, accN = 0, 0, 0
				speedT++
			}
		}
		lastRx, lastTx, haveLast = rx, tx, true
		d := dirty && time.Since(lastSave) > 30*time.Minute
		mu.Unlock()
		if d {
			saveUsage()
			lastSave = time.Now()
		}
	}
}

func proxyClient(t time.Duration) *http.Client {
	pu, _ := url.Parse("http://127.0.0.1:10855")
	return &http.Client{Timeout: t, Transport: &http.Transport{Proxy: http.ProxyURL(pu), DisableKeepAlives: true}}
}

var lastGood map[string]any
var lastGoodT time.Time

func probeIP(force bool) map[string]any {
	mu.Lock()
	if !force && ipCache != nil && time.Since(ipTime) < 25*time.Second {
		c := ipCache
		mu.Unlock()
		return c
	}
	mu.Unlock()
	res := map[string]any{"ok": false}
	if pidAlive(readPid(home + "/run/xray.pid")) {
		type rr struct {
			m  map[string]any
			ms int
		}
		ch := make(chan rr, 4)
		get := func(u string, parse func([]byte) map[string]any) {
			cl := proxyClient(9 * time.Second)
			t0 := time.Now()
			rq, _ := http.NewRequest("GET", u, nil)
			rq.Header.Set("User-Agent", "curl/8.5.0")
			r, err := cl.Do(rq)
			if err != nil {
				ch <- rr{}
				return
			}
			defer r.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			if r.StatusCode != 200 {
				ch <- rr{}
				return
			}
			ch <- rr{parse(b), int(time.Since(t0) / time.Millisecond)}
		}
		plain := func(b []byte) map[string]any {
			ip := strings.TrimSpace(string(b))
			if net.ParseIP(ip) == nil {
				return nil
			}
			return map[string]any{"ip": ip, "country": "", "cc": "", "isp": ""}
		}
		go get("http://ip-api.com/json/?fields=status,query,country,countryCode,isp", func(b []byte) map[string]any {
			var j map[string]any
			if json.Unmarshal(b, &j) != nil || j["status"] != "success" {
				return nil
			}
			return map[string]any{"ip": j["query"], "country": j["country"], "cc": j["countryCode"], "isp": j["isp"]}
		})
		go get("http://ipwho.is/", func(b []byte) map[string]any {
			var j map[string]any
			if json.Unmarshal(b, &j) != nil || j["success"] != true {
				return nil
			}
			isp := ""
			if c, ok := j["connection"].(map[string]any); ok {
				isp, _ = c["isp"].(string)
			}
			return map[string]any{"ip": j["ip"], "country": j["country"], "cc": j["country_code"], "isp": isp}
		})
		go get("http://ifconfig.me/ip", plain)
		go get("http://api.ipify.org/", plain)
		var best *rr
		for i := 0; i < 4; i++ {
			x := <-ch
			if x.m == nil {
				continue
			}
			if best == nil || (best.m["country"] == "" && x.m["country"] != "") {
				c := x
				best = &c
			}
			if best.m["country"] != "" {
				break
			}
		}
		if best != nil {
			res = map[string]any{"ok": true, "ip": best.m["ip"], "country": best.m["country"], "cc": best.m["cc"], "isp": best.m["isp"], "ms": best.ms}
			addLat(best.ms)
			mu.Lock()
			lastGood, lastGoodT = res, time.Now()
			mu.Unlock()
		} else {
			addLat(-1)
			mu.Lock()
			if lastGood != nil && time.Since(lastGoodT) < 15*time.Minute {
				res = lastGood
			}
			mu.Unlock()
		}
	} else {
		cl := &http.Client{Timeout: 7 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		if r, err := cl.Get("http://ip-api.com/json/?fields=status,query,country,countryCode,isp"); err == nil {
			b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			r.Body.Close()
			var j map[string]any
			if json.Unmarshal(b, &j) == nil && j["status"] == "success" {
				res = map[string]any{"ok": true, "ip": j["query"], "country": j["country"], "cc": j["countryCode"], "isp": j["isp"], "direct": true}
			}
		}
	}
	mu.Lock()
	ipCache, ipTime = res, time.Now()
	mu.Unlock()
	return res
}

func addLat(ms int) {
	mu.Lock()
	defer mu.Unlock()
	lat = append(lat, latP{time.Now().Unix(), ms})
	if len(lat) > 300 {
		lat = lat[len(lat)-300:]
	}
}

func prober() {
	for {
		time.Sleep(5 * time.Minute)
		if pidAlive(readPid(home + "/run/xray.pid")) {
			probeIP(true)
		}
	}
}

func tcpPing(host string, port int) int {
	t0 := time.Now()
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 3*time.Second)
	if err != nil {
		return -1
	}
	c.Close()
	return int(time.Since(t0) / time.Millisecond)
}

// Rekod WAN terputus (naik -> turun), disimpan dalam etc/wandrops.json.
var (
	dropMu    sync.Mutex
	drops     []int64
	wanWasUp  = true
	dropsInit bool
)

func trackWan(up bool) {
	dropMu.Lock()
	defer dropMu.Unlock()
	if !dropsInit {
		dropsInit = true
		if b, err := os.ReadFile(home + "/etc/wandrops.json"); err == nil {
			_ = json.Unmarshal(b, &drops)
		}
	}
	if wanWasUp && !up {
		drops = append(drops, time.Now().Unix())
		cut := time.Now().Add(-7 * 24 * time.Hour).Unix()
		for len(drops) > 0 && (drops[0] < cut || len(drops) > 200) {
			drops = drops[1:]
		}
		b, _ := json.Marshal(drops)
		_ = os.WriteFile(home+"/etc/wandrops.json", b, 0600)
	}
	wanWasUp = up
}

func dropCount(d time.Duration) int {
	dropMu.Lock()
	defer dropMu.Unlock()
	cut := time.Now().Add(-d).Unix()
	n := 0
	for _, x := range drops {
		if x >= cut {
			n++
		}
	}
	return n
}

func dnsServers() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range []string{"/tmp/resolv.conf.auto", "/tmp/resolv.conf.d/resolv.conf.auto", "/tmp/resolv.conf", "/etc/resolv.conf"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			fl := strings.Fields(l)
			if len(fl) >= 2 && fl[0] == "nameserver" {
				ip := fl[1]
				if i := strings.Index(ip, "%"); i > 0 {
					ip = ip[:i]
				}
				if net.ParseIP(ip) == nil || strings.HasPrefix(ip, "127.") || ip == "::1" || seen[ip] {
					continue
				}
				seen[ip] = true
				out = append(out, ip)
			}
		}
		if len(out) > 0 {
			break
		}
	}
	return out
}
