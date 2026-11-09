package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sekat iklan peringkat DNS. Soalan DNS pelanggan LAN dialihkan (iptables REDIRECT) ke penapis kecil ini.
// Domain dalam senarai dijawab 0.0.0.0 / :: dan dikira; soalan lain dihantar semula tanpa perubahan
// ke dnsmasq modem (atau ke dns-in xray jika etc/dnsredir wujud).

const adPort = 10853

type AdCfg struct {
	On    bool     `json:"on"`
	Lists []string `json:"lists"` // id senarai yang dihidupkan
	White []string `json:"white"`
	Black []string `json:"black"`
}

type adSrc struct {
	ID, Name, URL string
}

var adSources = []adSrc{
	{"adguard", "AdGuard DNS filter", "https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt"},
	{"stevenblack", "StevenBlack (iklan dan penjejak)", "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"},
}

func defAd() AdCfg { return AdCfg{Lists: []string{"adguard", "stevenblack"}} }

func loadAd() AdCfg {
	c := defAd()
	if b, err := os.ReadFile(home + "/etc/adblock.json"); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return normAd(c)
}

func cleanDomains(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d), "*.")))
		d = strings.TrimSuffix(d, ".")
		if d == "" || !domRe.MatchString(d) || !strings.Contains(d, ".") || seen[d] || len(out) >= 500 {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

func normAd(c AdCfg) AdCfg {
	ls := []string{}
	for _, s := range adSources {
		for _, id := range c.Lists {
			if id == s.ID {
				ls = append(ls, id)
				break
			}
		}
	}
	c.Lists = ls
	c.White = cleanDomains(c.White)
	c.Black = cleanDomains(c.Black)
	return c
}

// ---- padanan domain ----

func h64(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

type blocker struct {
	set   map[uint64]struct{}
	white map[uint64]struct{}
}

// sufiks: ads.x.example.com -> ads.x.example.com, x.example.com, example.com
func suffixes(name string, f func(string) bool) bool {
	for {
		if f(name) {
			return true
		}
		i := strings.IndexByte(name, '.')
		if i < 0 || i == len(name)-1 {
			return false
		}
		name = name[i+1:]
		if !strings.Contains(name, ".") {
			return false // jangan padan TLD sahaja
		}
	}
}

func (b *blocker) match(name string) bool {
	if b == nil || len(b.set) == 0 {
		return false
	}
	if len(b.white) > 0 && suffixes(name, func(s string) bool { _, ok := b.white[h64(s)]; return ok }) {
		return false
	}
	return suffixes(name, func(s string) bool { _, ok := b.set[h64(s)]; return ok })
}

// parseAdLine mengeluarkan domain daripada baris format hosts atau Adblock (||domain^).
func parseAdLine(l string) string {
	l = strings.TrimSpace(l)
	if l == "" || l[0] == '#' || l[0] == '!' || l[0] == '[' || strings.HasPrefix(l, "@@") || l[0] == '/' {
		return ""
	}
	if strings.HasPrefix(l, "||") {
		l = l[2:]
		end := strings.IndexAny(l, "^$/|")
		if end < 0 {
			return ""
		}
		if l[end] != '^' || (len(l) > end+1 && l[end+1:] != "" && l[end+1] != '$') {
			return "" // peraturan berlaluan atau bukan domain sahaja
		}
		l = l[:end]
	} else {
		f := strings.Fields(l)
		if len(f) < 2 || (f[0] != "0.0.0.0" && f[0] != "127.0.0.1") {
			return ""
		}
		l = f[1]
	}
	l = strings.ToLower(strings.TrimSuffix(l, "."))
	if l == "" || strings.ContainsAny(l, "*:/") || !domRe.MatchString(l) || !strings.Contains(l, ".") || l == "localhost" || l == "local" {
		return ""
	}
	return l
}

func adFile(id string) string { return home + "/etc/ads/" + id + ".txt" }

var (
	adMu     sync.RWMutex
	adBlk    *blocker
	adCounts = map[string]int{} // bilangan domain setiap senarai
	adBuild  sync.Mutex
)

func adRebuild() {
	adBuild.Lock()
	defer adBuild.Unlock()
	c := loadAd()
	b := &blocker{set: map[uint64]struct{}{}, white: map[uint64]struct{}{}}
	counts := map[string]int{}
	for _, id := range c.Lists {
		f, err := os.Open(adFile(id))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		n := 0
		for sc.Scan() {
			if d := parseAdLine(sc.Text()); d != "" {
				b.set[h64(d)] = struct{}{}
				n++
			}
		}
		f.Close()
		counts[id] = n
	}
	for _, d := range c.Black {
		b.set[h64(d)] = struct{}{}
	}
	for _, d := range c.White {
		b.white[h64(d)] = struct{}{}
	}
	adMu.Lock()
	adBlk, adCounts = b, counts
	adMu.Unlock()
	debug.FreeOSMemory()
}

// ---- kemas kini senarai ----

var adUp struct {
	sync.Mutex
	run bool
	msg string
}

func adUpdate() {
	adUp.Lock()
	if adUp.run {
		adUp.Unlock()
		return
	}
	adUp.run, adUp.msg = true, "Bermula..."
	adUp.Unlock()
	set := func(m string) { adUp.Lock(); adUp.msg = m; adUp.Unlock() }
	defer func() { adUp.Lock(); adUp.run = false; adUp.Unlock() }()
	_ = os.MkdirAll(home+"/etc/ads", 0700)
	cl := &http.Client{Timeout: 3 * time.Minute}
	failed := 0
	for _, id := range loadAd().Lists {
		var src adSrc
		for _, s := range adSources {
			if s.ID == id {
				src = s
			}
		}
		set("Memuat turun " + src.Name + "...")
		resp, err := cl.Get(src.URL)
		if err != nil {
			set("Gagal " + src.Name + ": " + shortErr(err))
			failed++
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			set(fmt.Sprintf("Gagal %s: HTTP %d", src.Name, resp.StatusCode))
			failed++
			continue
		}
		tmp := adFile(id) + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			resp.Body.Close()
			set("Gagal tulis: " + err.Error())
			return
		}
		n, err := io.Copy(f, io.LimitReader(resp.Body, 20<<20))
		f.Close()
		resp.Body.Close()
		if err != nil || n < 10000 {
			_ = os.Remove(tmp)
			set("Gagal " + src.Name + ": fail tidak lengkap")
			failed++
			continue
		}
		_ = os.Rename(tmp, adFile(id))
	}
	adRebuild()
	if failed == 0 {
		set("Siap")
	}
}

// ---- statistik ----

type adDay struct {
	D string `json:"d"`
	Q int    `json:"q"`
	B int    `json:"b"`
}

type adRecent struct {
	T int64  `json:"t"`
	D string `json:"d"`
}

type adStats struct {
	AllQ   int            `json:"allq"`
	AllB   int            `json:"allb"`
	Days   []adDay        `json:"days"`
	Top    map[string]int `json:"top"`
	Recent []adRecent     `json:"recent"`
}

var (
	stMu    sync.Mutex
	stData  = adStats{Top: map[string]int{}}
	stDirty bool
)

func adStatLoad() {
	stMu.Lock()
	defer stMu.Unlock()
	if b, err := os.ReadFile(home + "/etc/adstats.json"); err == nil {
		_ = json.Unmarshal(b, &stData)
	}
	if stData.Top == nil {
		stData.Top = map[string]int{}
	}
}

func adStatSave() {
	stMu.Lock()
	if !stDirty {
		stMu.Unlock()
		return
	}
	b, _ := json.Marshal(stData)
	stDirty = false
	stMu.Unlock()
	_ = os.WriteFile(home+"/etc/adstats.json", b, 0600)
}

func adCount(name string, blocked bool) {
	day := time.Now().Format("2006-01-02")
	stMu.Lock()
	defer stMu.Unlock()
	if n := len(stData.Days); n == 0 || stData.Days[n-1].D != day {
		stData.Days = append(stData.Days, adDay{D: day})
		if len(stData.Days) > 14 {
			stData.Days = stData.Days[len(stData.Days)-14:]
		}
	}
	d := &stData.Days[len(stData.Days)-1]
	d.Q++
	stData.AllQ++
	if blocked {
		d.B++
		stData.AllB++
		if _, ok := stData.Top[name]; ok || len(stData.Top) < 3000 {
			stData.Top[name]++
		}
		stData.Recent = append(stData.Recent, adRecent{T: time.Now().Unix(), D: name})
		if len(stData.Recent) > 20 {
			stData.Recent = stData.Recent[len(stData.Recent)-20:]
		}
	}
	stDirty = true
}

// ---- DNS ----

// dnsQuestion mengurai soalan pertama. qend = hujung bahagian soalan.
func dnsQuestion(m []byte) (name string, qtype uint16, qend int, ok bool) {
	if len(m) < 12 || m[2]&0x80 != 0 || binary.BigEndian.Uint16(m[4:6]) != 1 {
		return
	}
	i := 12
	var parts []string
	for {
		if i >= len(m) {
			return
		}
		l := int(m[i])
		if l == 0 {
			i++
			break
		}
		if l&0xC0 != 0 || i+1+l > len(m) {
			return
		}
		parts = append(parts, string(m[i+1:i+1+l]))
		i += 1 + l
	}
	if i+4 > len(m) {
		return
	}
	return strings.ToLower(strings.Join(parts, ".")), binary.BigEndian.Uint16(m[i : i+2]), i + 4, true
}

// dnsBlockedReply: jawapan 0.0.0.0 / :: untuk A/AAAA, kosong (NODATA) untuk jenis lain.
func dnsBlockedReply(q []byte, qtype uint16, qend int) []byte {
	r := make([]byte, qend, qend+28)
	copy(r, q[:qend])
	r[2] = 0x80 | (q[2] & 0x79) | 0 // QR=1, salin opcode dan RD
	r[2] |= q[2] & 0x01
	r[3] = 0x80 // RA, rcode 0
	binary.BigEndian.PutUint16(r[6:], 0)
	binary.BigEndian.PutUint16(r[8:], 0)
	binary.BigEndian.PutUint16(r[10:], 0)
	var rd []byte
	switch qtype {
	case 1:
		rd = make([]byte, 4)
	case 28:
		rd = make([]byte, 16)
	}
	if rd == nil {
		return r
	}
	binary.BigEndian.PutUint16(r[6:], 1)
	r = append(r, 0xC0, 0x0C, byte(qtype>>8), byte(qtype), 0, 1, 0, 0, 0, 60, byte(len(rd)>>8), byte(len(rd)))
	return append(r, rd...)
}

func adUpstreams() []string {
	if _, err := os.Stat(home + "/etc/dnsredir"); err == nil {
		return []string{"127.0.0.1:10854"}
	}
	u := []string{"127.0.0.1:53"}
	if ip := ifaceIP("br-lan"); ip != "" {
		u = append(u, ip+":53")
	}
	return u
}

func fwdUDP(q []byte) []byte {
	for _, up := range adUpstreams() {
		c, err := net.DialTimeout("udp", up, 2*time.Second)
		if err != nil {
			continue
		}
		_ = c.SetDeadline(time.Now().Add(4 * time.Second))
		if _, err = c.Write(q); err == nil {
			buf := make([]byte, 4096)
			if n, err := c.Read(buf); err == nil {
				c.Close()
				return buf[:n]
			}
		}
		c.Close()
	}
	return nil
}

func servfail(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	r := append([]byte{}, q...)
	r[2] |= 0x80
	r[3] = 0x82 // RA + SERVFAIL
	return r
}

func adAnswer(q []byte) []byte {
	name, qt, qend, ok := dnsQuestion(q)
	if !ok {
		return fwdUDP(q)
	}
	if name == "vp-health.invalid" {
		return dnsBlockedReply(q, qt, qend)
	}
	adMu.RLock()
	b := adBlk
	adMu.RUnlock()
	if b.match(name) {
		adCount(name, true)
		return dnsBlockedReply(q, qt, qend)
	}
	adCount(name, false)
	if r := fwdUDP(q); r != nil {
		return r
	}
	return servfail(q)
}

type adServer struct {
	pcs []net.PacketConn
	lns []net.Listener
	sem chan struct{}
	v6  bool
}

func (s *adServer) servePC(pc net.PacketConn) {
	buf := make([]byte, 4096)
	for {
		n, a, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		q := append([]byte{}, buf[:n]...)
		select {
		case s.sem <- struct{}{}:
			go func() {
				defer func() { <-s.sem }()
				if r := adAnswer(q); r != nil {
					_, _ = pc.WriteTo(r, a)
				}
			}()
		default:
		}
	}
}

func (s *adServer) serveLN(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go adTCP(c)
	}
}

func adStart() (*adServer, error) {
	ip := ifaceIP("br-lan")
	if ip == "" {
		return nil, fmt.Errorf("br-lan tiada IP")
	}
	addr := fmt.Sprintf("%s:%d", ip, adPort)
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		pc.Close()
		return nil, err
	}
	s := &adServer{pcs: []net.PacketConn{pc}, lns: []net.Listener{ln}, sem: make(chan struct{}, 64)}
	// IPv6 (telefon sering hantar soalan DNS melalui IPv6). Cuba terbaik; jika gagal, IPv4 sahaja.
	a6 := fmt.Sprintf("[::]:%d", adPort)
	if pc6, e1 := net.ListenPacket("udp6", a6); e1 == nil {
		if ln6, e2 := net.Listen("tcp6", a6); e2 == nil {
			s.pcs = append(s.pcs, pc6)
			s.lns = append(s.lns, ln6)
			s.v6 = true
		} else {
			pc6.Close()
		}
	}
	for _, p := range s.pcs {
		go s.servePC(p)
	}
	for _, l := range s.lns {
		go s.serveLN(l)
	}
	return s, nil
}

func (s *adServer) stop() {
	if s != nil {
		for _, p := range s.pcs {
			p.Close()
		}
		for _, l := range s.lns {
			l.Close()
		}
	}
}

func adTCP(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(8 * time.Second))
	var lb [2]byte
	if _, err := io.ReadFull(c, lb[:]); err != nil {
		return
	}
	l := int(binary.BigEndian.Uint16(lb[:]))
	q := make([]byte, l)
	if _, err := io.ReadFull(c, q); err != nil {
		return
	}
	var r []byte
	name, qt, qend, ok := dnsQuestion(q)
	adMu.RLock()
	b := adBlk
	adMu.RUnlock()
	if ok && b.match(name) {
		adCount(name, true)
		r = dnsBlockedReply(q, qt, qend)
	} else {
		if ok {
			adCount(name, false)
		}
		for _, up := range adUpstreams() {
			u, err := net.DialTimeout("tcp", up, 2*time.Second)
			if err != nil {
				continue
			}
			_ = u.SetDeadline(time.Now().Add(6 * time.Second))
			_, _ = u.Write(append(lb[:], q...))
			var rb [2]byte
			if _, err := io.ReadFull(u, rb[:]); err == nil {
				r = make([]byte, binary.BigEndian.Uint16(rb[:]))
				if _, err := io.ReadFull(u, r); err != nil {
					r = nil
				}
			}
			u.Close()
			if r != nil {
				break
			}
		}
		if r == nil {
			r = servfail(q)
		}
	}
	if r != nil {
		out := make([]byte, 2, 2+len(r))
		binary.BigEndian.PutUint16(out, uint16(len(r)))
		_, _ = c.Write(append(out, r...))
	}
}

// ---- iptables dan pengurusan ----

func adRuleArgs(op, proto string) []string {
	return []string{"-t", "nat", op, "PREROUTING", "-i", "br-lan", "-p", proto, "--dport", "53", "-j", "REDIRECT", "--to-ports", fmt.Sprint(adPort)}
}

func adRules(want bool) { adRules2(want, false) }

// adRules2: v6 = peraturan ip6tables (best-effort; abaikan jika kernel tidak menyokong).
func adRules2(want, v6 bool) {
	ipt := "iptables"
	if v6 {
		ipt = "ip6tables"
	}
	for _, p := range []string{"udp", "tcp"} {
		_, err := run2(ipt, adRuleArgs("-C", p)...)
		has := err == nil
		switch {
		case want && !has:
			a := adRuleArgs("-I", p)
			a = append(a[:4], append([]string{"1"}, a[4:]...)...) // -t nat -I PREROUTING 1 ...
			_, _ = run2(ipt, a...)
		case !want && has:
			for k := 0; k < 3; k++ {
				if _, err := run2(ipt, adRuleArgs("-D", p)...); err != nil {
					break
				}
			}
		}
	}
}

// adHealthy: hantar soalan ujian kepada penapis sendiri.
func adHealthy() bool {
	ip := ifaceIP("br-lan")
	if ip == "" {
		return false
	}
	q := []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, p := range strings.Split("vp-health.invalid", ".") {
		q = append(q, byte(len(p)))
		q = append(q, p...)
	}
	q = append(q, 0, 0, 1, 0, 1)
	c, err := net.DialTimeout("udp", fmt.Sprintf("%s:%d", ip, adPort), time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write(q); err != nil {
		return false
	}
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	return err == nil && n > 12 && buf[0] == 0x12 && buf[1] == 0x34
}

func memTotalMB() uint64 { t, _ := meminfo(); return t / 1024 }

func adWant() bool {
	return loadAd().On && !locked(readLic()) && memTotalMB() >= 512 &&
		readStr(home+"/etc/mode", "xlite") == "vless" && vlessUp()
}

var adActiveNow bool

func adActive() bool { return adActiveNow }

func adManager() {
	adStatLoad()
	adRebuild()
	var srv *adServer
	lastSave := time.Now()
	for {
		want := adWant()
		if want && srv == nil {
			debug.SetMemoryLimit(48 << 20)
			if s, err := adStart(); err == nil {
				srv = s
			} else {
				fmt.Fprintln(os.Stderr, "sekat iklan:", err)
			}
		}
		if !want && srv != nil {
			adRules(false)
			adRules2(false, true)
			srv.stop()
			srv = nil
			debug.SetMemoryLimit(14 << 20)
		}
		on := srv != nil && adHealthy()
		if srv != nil {
			adRules(on)
			adRules2(on && srv.v6, true)
		}
		adActiveNow = on && srv != nil
		if time.Since(lastSave) > 2*time.Minute {
			adStatSave()
			lastSave = time.Now()
		}
		// kemas kini senarai setiap 7 hari
		if want {
			for _, id := range loadAd().Lists {
				st, err := os.Stat(adFile(id))
				if err != nil || time.Since(st.ModTime()) > 7*24*time.Hour {
					go adUpdate()
					break
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
}

// ---- API ----

func regAd(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/adblock", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if locked(readLic()) {
				jr(w, map[string]any{"ok": false, "msg": "Lesen belum aktif"})
				return
			}
			var c AdCfg
			if json.Unmarshal(body(r, 1<<17), &c) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Data tidak sah"})
				return
			}
			if c.On && memTotalMB() < 512 {
				jr(w, map[string]any{"ok": false, "msg": "RAM peranti tidak mencukupi untuk sekat iklan"})
				return
			}
			c = normAd(c)
			if c.On && len(c.Lists) == 0 && len(c.Black) == 0 {
				jr(w, map[string]any{"ok": false, "msg": "Pilih sekurang-kurangnya satu senarai"})
				return
			}
			b, _ := json.Marshal(c)
			if os.WriteFile(home+"/etc/adblock.json", b, 0600) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Gagal simpan"})
				return
			}
			missing := false
			for _, id := range c.Lists {
				if _, err := os.Stat(adFile(id)); err != nil {
					missing = true
				}
			}
			adRebuild()
			msg := "Disimpan"
			if c.On && missing {
				go adUpdate()
				msg = "Memuat turun senarai iklan..."
			}
			jr(w, map[string]any{"ok": true, "msg": msg})
			return
		}
		adMu.RLock()
		counts := map[string]int{}
		total := 0
		for k, v := range adCounts {
			counts[k] = v
			total += v
		}
		adMu.RUnlock()
		lists := []map[string]any{}
		for _, s := range adSources {
			e := map[string]any{"id": s.ID, "name": s.Name, "n": counts[s.ID], "at": int64(0)}
			if st, err := os.Stat(adFile(s.ID)); err == nil {
				e["at"] = st.ModTime().Unix()
			}
			lists = append(lists, e)
		}
		type kv struct {
			D string `json:"d"`
			N int    `json:"n"`
		}
		stMu.Lock()
		top := []kv{}
		for d, n := range stData.Top {
			top = append(top, kv{d, n})
		}
		sort.Slice(top, func(i, j int) bool { return top[i].N > top[j].N })
		if len(top) > 10 {
			top = top[:10]
		}
		recent := append([]adRecent{}, stData.Recent...)
		for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
			recent[i], recent[j] = recent[j], recent[i]
		}
		today := adDay{D: time.Now().Format("2006-01-02")}
		if n := len(stData.Days); n > 0 && stData.Days[n-1].D == today.D {
			today = stData.Days[n-1]
		}
		st := map[string]any{"today": today, "allq": stData.AllQ, "allb": stData.AllB, "days": stData.Days, "top": top, "recent": recent}
		stMu.Unlock()
		adUp.Lock()
		run, msg := adUp.run, adUp.msg
		adUp.Unlock()
		jr(w, map[string]any{"cfg": loadAd(), "active": adActive(), "ram": memTotalMB() >= 512, "total": total,
			"lists": lists, "run": run, "msg": msg, "stats": st,
			"mode": readStr(home+"/etc/mode", "xlite") == "vless" && vlessUp()})
	})
	act("/api/adblock/update", func(r *http.Request) (string, bool) {
		adUp.Lock()
		run := adUp.run
		adUp.Unlock()
		if run {
			return "Sedang berjalan", true
		}
		go adUpdate()
		return "Kemas kini dimulakan", true
	})
	act("/api/adblock/reset", func(r *http.Request) (string, bool) {
		stMu.Lock()
		stData = adStats{Top: map[string]int{}}
		stDirty = true
		stMu.Unlock()
		adStatSave()
		return "Statistik ditetapkan semula", true
	})
}
