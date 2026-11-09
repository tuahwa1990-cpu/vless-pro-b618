package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	pass    string
	passMu  sync.Mutex
	started = time.Now()
)

// sessNonce berubah pada setiap permulaan proses, jadi mula semula servis memaksa log masuk semula.
var sessNonce = func() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}()

func tokenOf(p string) string {
	h := sha256.Sum256([]byte("vp|" + version + "|" + sessNonce + "|" + p))
	return hex.EncodeToString(h[:])
}

func vlessUp() bool { return pidAlive(readPid(home + "/run/xray.pid")) }

func restartIfActive() {
	if readStr(home+"/etc/mode", "xlite") == "vless" {
		runVP(true, "restart")
	}
}

func meminfo() (tot, free uint64) {
	b, _ := os.ReadFile("/proc/meminfo")
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			tot = v
		case "MemFree:", "Buffers:", "Cached:":
			free += v
		}
	}
	return
}

func gateway() string {
	b, _ := os.ReadFile("/proc/net/route")
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) > 2 && f[1] == "00000000" {
			v, _ := strconv.ParseUint(f[2], 16, 32)
			ip := make(net.IP, 4)
			binary.LittleEndian.PutUint32(ip, uint32(v))
			return ip.String()
		}
	}
	return ""
}

func ifaceIP(n string) string {
	ifc, err := net.InterfaceByName(n)
	if err != nil {
		return ""
	}
	as, _ := ifc.Addrs()
	for _, a := range as {
		if ip, _, err := net.ParseCIDR(a.String()); err == nil && ip.To4() != nil {
			return ip.String()
		}
	}
	return ""
}

func hookOn() bool {
	f := os.Getenv("VP_AUTORUN")
	if f == "" {
		f = "/system/etc/autorun.sh"
	}
	b, _ := os.ReadFile(f)
	return strings.Contains(string(b), "vlesspro-hook")
}

//go:embed ui.html
var uiFS embed.FS

func serve() {
	runtime.GOMAXPROCS(1)
	debug.SetGCPercent(40)
	debug.SetMemoryLimit(14 << 20)
	_ = os.MkdirAll(home+"/run", 0700)
	pass = readStr(home+"/etc/pass", "")
	if pass == "" {
		fmt.Fprintln(os.Stderr, "etc/pass tiada")
		os.Exit(1)
	}
	listen := readStr(home+"/etc/listen", "192.168.8.1:8990")
	go sampler()
	go prober()
	go watchdog()
	go sqmWatch()
	go quotaLoop()
	go adManager()
	go rebootSched()
	mux := http.NewServeMux()
	ui, _ := uiFS.ReadFile("ui.html")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(ui)
	})
	body := func(r *http.Request, n int64) []byte { b, _ := io.ReadAll(io.LimitReader(r.Body, n)); return b }
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		l := readLic()
		_, name, _ := activeParsed()
		var since int64
		if st, err := os.Stat(home + "/run/xray.pid"); err == nil && vlessUp() {
			since = st.ModTime().Unix()
		}
		mu.Lock()
		rx, tx := rateRx, rateTx
		mu.Unlock()
		jr(w, map[string]any{"mustchange": mustChange(), "version": version, "mode": readStr(home+"/etc/mode", "xlite"),
			"vless": vlessUp(), "xlite": xliteRunning(), "hasx": xliteInstalled(), "node": name, "lic": l, "locked": locked(l),
			"since": since, "now": time.Now().Unix(), "rx": rx, "tx": tx})
	})
	mux.HandleFunc("/api/ip", func(w http.ResponseWriter, r *http.Request) {
		jr(w, probeIP(r.URL.Query().Get("force") == "1"))
	})
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		tot, free := meminfo()
		ifn := wanIface()
		rx, tx, _ := netBytes(ifn)
		jr(w, map[string]any{"iface": ifn, "wanip": ifaceIP(ifn), "gw": gateway(),
			"memtot": tot, "memfree": free, "rx": rx, "tx": tx,
			"dns": dnsServers(), "drops1h": dropCount(time.Hour), "drops24h": dropCount(24 * time.Hour)})
	})
	mux.HandleFunc("/api/graphs", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		jr(w, map[string]any{"speed": speed[:], "live": live[:], "usage": usage, "lat": lat, "now": time.Now().Unix()})
	})
	mux.HandleFunc("/api/lic", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			b := body(r, 4096)
			l := checkKey(string(b))
			if l.State == "active" {
				_ = os.WriteFile(home+"/etc/license", []byte(strings.Join(strings.Fields(string(b)), "")+"\n"), 0600)
			}
			jr(w, l)
			return
		}
		jr(w, readLic())
	})
	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		out := []map[string]any{}
		sel := selIdx()
		if selGroup() >= 0 {
			sel = -1
		}
		for i, n := range loadNodes() {
			if p, err := parseLink(n.Link); err == nil {
				out = append(out, map[string]any{"i": i, "id": nid(n.Link), "name": p.Name, "host": mask(p.Host), "port": p.Port,
					"net": p.Net, "sec": orStr(p.Sec, "none"), "sel": i == sel, "src": n.Src})
			}
		}
		jr(w, out)
	})
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		ns := loadNodes()
		res := make([]int, len(ns))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)
		for i, n := range ns {
			p, err := parseLink(n.Link)
			if err != nil {
				res[i] = -1
				continue
			}
			wg.Add(1)
			go func(i int, h string, pt int) {
				defer wg.Done()
				sem <- struct{}{}
				res[i] = tcpPing(h, pt)
				<-sem
			}(i, p.Host, p.Port)
		}
		wg.Wait()
		jr(w, res)
	})
	act := func(path string, f func(*http.Request) (string, bool)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "POST sahaja", 405)
				return
			}
			if locked(readLic()) {
				jr(w, map[string]any{"ok": false, "msg": "Lesen belum aktif"})
				return
			}
			m, ok := f(r)
			jr(w, map[string]any{"ok": ok, "msg": m})
		})
	}
	act("/api/node/add", func(r *http.Request) (string, bool) {
		var added, bad int
		ns := loadNodes()
		for _, ln := range strings.Fields(string(body(r, 65536))) {
			if p, err := parseLink(ln); err == nil {
				ns = append(ns, Node{Name: p.Name, Link: ln})
				added++
			} else {
				bad++
			}
		}
		if added > 0 {
			_ = saveNodes(ns)
		}
		return fmt.Sprintf("Ditambah %d, ditolak %d", added, bad), added > 0
	})
	act("/api/node/del", func(r *http.Request) (string, bool) {
		i, err := strconv.Atoi(strings.TrimSpace(string(body(r, 64))))
		ns := loadNodes()
		if err != nil || i < 0 || i >= len(ns) {
			return "Indeks tidak sah", false
		}
		sel := selIdx()
		ns = append(ns[:i], ns[i+1:]...)
		_ = saveNodes(ns)
		if sel > i || sel >= len(ns) {
			_ = setSel(max0(sel - 1))
		}
		restartIfActive()
		return "Dipadam", true
	})
	act("/api/node/use", func(r *http.Request) (string, bool) {
		i, err := strconv.Atoi(strings.TrimSpace(string(body(r, 64))))
		if err != nil || i < 0 || i >= len(loadNodes()) {
			return "Indeks tidak sah", false
		}
		_ = setSel(i)
		setSelGroup(-1)
		restartIfActive()
		return "Profil dipilih", true
	})
	mux.HandleFunc("/api/groups", func(w http.ResponseWriter, r *http.Request) {
		gs := loadGroups()
		nm := map[string]string{}
		for _, n := range loadNodes() {
			if p, err := parseLink(n.Link); err == nil {
				nm[nid(n.Link)] = p.Name
			}
		}
		sel := selGroup()
		out := []map[string]any{}
		for i, g := range gs {
			names := []string{}
			for _, m := range g.Members {
				if v, ok := nm[m]; ok {
					names = append(names, v)
				}
			}
			out = append(out, map[string]any{"i": i, "name": g.Name, "members": names, "sel": i == sel, "strat": g.Strat})
		}
		jr(w, out)
	})
	act("/api/group/add", func(r *http.Request) (string, bool) {
		var g Group
		if json.Unmarshal(body(r, 16384), &g) != nil || strings.TrimSpace(g.Name) == "" || len(g.Members) < 2 {
			return "Nama dan sekurang-kurangnya 2 profil diperlukan", false
		}
		if g.Strat != "random" && g.Strat != "roundRobin" {
			g.Strat = "leastPing"
		}
		_ = saveGroups(append(loadGroups(), g))
		return "Kumpulan dicipta", true
	})
	act("/api/group/del", func(r *http.Request) (string, bool) {
		i, err := strconv.Atoi(strings.TrimSpace(string(body(r, 64))))
		gs := loadGroups()
		if err != nil || i < 0 || i >= len(gs) {
			return "Indeks tidak sah", false
		}
		gs = append(gs[:i], gs[i+1:]...)
		_ = saveGroups(gs)
		if sg := selGroup(); sg == i {
			setSelGroup(-1)
			restartIfActive()
		} else if sg > i {
			setSelGroup(sg - 1)
		}
		return "Dipadam", true
	})
	act("/api/group/use", func(r *http.Request) (string, bool) {
		i, err := strconv.Atoi(strings.TrimSpace(string(body(r, 64))))
		if err != nil || i < 0 || i >= len(loadGroups()) {
			return "Indeks tidak sah", false
		}
		setSelGroup(i)
		restartIfActive()
		return "Kumpulan dipilih", true
	})
	mux.HandleFunc("/api/subs", func(w http.ResponseWriter, r *http.Request) {
		out := []map[string]any{}
		for i, s := range loadSubs() {
			out = append(out, map[string]any{"i": i, "name": s.Name, "count": s.Count, "updated": s.Updated})
		}
		jr(w, out)
	})
	act("/api/sub/add", func(r *http.Request) (string, bool) {
		var s Sub
		if json.Unmarshal(body(r, 4096), &s) != nil || !strings.HasPrefix(s.URL, "http") {
			return "URL langganan tidak sah", false
		}
		s.URL = strings.TrimSpace(s.URL)
		if s.Name == "" {
			s.Name = "Langganan"
		}
		s.ID = nid(s.URL)
		subs := loadSubs()
		for _, x := range subs {
			if x.ID == s.ID {
				return "Langganan sudah ada", false
			}
		}
		if err := refreshSub(&s); err != nil {
			return "Gagal: " + err.Error(), false
		}
		saveSubs(append(subs, s))
		restartIfActive()
		return fmt.Sprintf("%d profil diimport", s.Count), true
	})
	act("/api/sub/refresh", func(r *http.Request) (string, bool) {
		i, err := strconv.Atoi(strings.TrimSpace(string(body(r, 64))))
		subs := loadSubs()
		if err != nil || i < 0 || i >= len(subs) {
			return "Indeks tidak sah", false
		}
		if err := refreshSub(&subs[i]); err != nil {
			return "Gagal: " + err.Error(), false
		}
		saveSubs(subs)
		restartIfActive()
		return fmt.Sprintf("%d profil dikemas kini", subs[i].Count), true
	})
	act("/api/sub/del", func(r *http.Request) (string, bool) {
		i, err := strconv.Atoi(strings.TrimSpace(string(body(r, 64))))
		subs := loadSubs()
		if err != nil || i < 0 || i >= len(subs) {
			return "Indeks tidak sah", false
		}
		id := subs[i].ID
		var keep []Node
		for _, n := range loadNodes() {
			if n.Src != id {
				keep = append(keep, n)
			}
		}
		_ = saveNodes(keep)
		_ = setSel(0)
		saveSubs(append(subs[:i], subs[i+1:]...))
		restartIfActive()
		return "Langganan dan profilnya dipadam", true
	})
	mux.HandleFunc("/api/direct", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if locked(readLic()) {
				jr(w, map[string]any{"ok": false, "msg": "Lesen belum aktif"})
				return
			}
			t := strings.Join(strings.Fields(strings.ReplaceAll(string(body(r, 16384)), ",", " ")), "\n")
			_ = os.WriteFile(home+"/etc/direct.txt", []byte(t+"\n"), 0600)
			restartIfActive()
			jr(w, map[string]any{"ok": true, "msg": "Disimpan"})
			return
		}
		jr(w, map[string]any{"text": strings.Join(directList(), "\n")})
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		jr(w, map[string]any{"hook": hookOn(), "version": version, "arch": runtime.GOARCH, "mac": macAddr(), "xrayhave": true, "id": hyph(devID20())})
	})
	act("/api/hook", func(r *http.Request) (string, bool) {
		if strings.TrimSpace(string(body(r, 16))) == "on" {
			runVP(true, "hook-install")
			return "Suis boot dipasang", true
		}
		runVP(true, "hook-remove")
		return "Suis boot dibuang", true
	})
	mux.HandleFunc("/api/pass", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST", 405)
			return
		}
		np := strings.TrimSpace(string(body(r, 128)))
		if len(np) < 6 {
			jr(w, map[string]any{"ok": false, "msg": "Minimum 6 aksara"})
			return
		}
		passMu.Lock()
		same := subtle.ConstantTimeCompare([]byte(np), []byte(pass)) == 1
		passMu.Unlock()
		if same && mustChange() {
			jr(w, map[string]any{"ok": false, "msg": "Kata laluan baharu mesti berbeza daripada yang lama"})
			return
		}
		passMu.Lock()
		pass = np
		passMu.Unlock()
		_ = os.WriteFile(home+"/etc/pass", []byte(np+"\n"), 0600)
		_ = os.Remove(home + "/etc/pass_initial")
		jr(w, map[string]any{"ok": true, "msg": "Kata laluan ditukar", "token": tokenOf(np)})
	})
	mux.HandleFunc("/api/system", func(w http.ResponseWriter, r *http.Request) {
		tot, free := meminfo()
		var st syscall.Statfs_t
		var dtot, dfree uint64
		if syscall.Statfs(home, &st) == nil {
			dtot, dfree = st.Blocks*uint64(st.Bsize), st.Bavail*uint64(st.Bsize)
		}
		up := ""
		if b, err := os.ReadFile("/proc/uptime"); err == nil {
			up = strings.Fields(string(b))[0]
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		jr(w, map[string]any{"memtot": tot, "memfree": free, "disktot": dtot, "diskfree": dfree,
			"uptime": up, "load": readStr("/proc/loadavg", ""), "uirss": ms.Sys, "uiup": int(time.Since(started).Seconds()),
			"xray": vlessUp(), "xlite": xliteRunning(), "arch": runtime.GOARCH})
	})
	mux.HandleFunc("/api/update", func(w http.ResponseWriter, r *http.Request) {
		lv, err := latestVer()
		if err != nil {
			jr(w, map[string]any{"ok": false, "cur": version, "msg": "Tidak dapat semak: " + shortErr(err)})
			return
		}
		jr(w, map[string]any{"ok": true, "cur": version, "latest": lv, "newer": lv != "" && verCmp(lv, version) > 0, "older": lv != "" && verCmp(lv, version) < 0, "st": upGet(), "prev": hasRollback()})
	})
	mux.HandleFunc("/api/update/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			jr(w, map[string]any{"ok": false, "msg": "Guna POST"})
			return
		}
		upMu.Lock()
		busy := upRun
		if !busy {
			upRun = true
		}
		upMu.Unlock()
		if busy {
			jr(w, map[string]any{"ok": false, "msg": "Kemas kini sedang berjalan"})
			return
		}
		fail := func(msg string) {
			upMu.Lock()
			upRun = false
			upMu.Unlock()
			jr(w, map[string]any{"ok": false, "msg": msg})
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, maxPkg+1))
		if err != nil || len(data) > maxPkg {
			fail("Fail terlalu besar atau tidak dapat dibaca")
			return
		}
		ver, vp, vpro, err := verifyPackage(data)
		if err != nil {
			fail(err.Error())
			return
		}
		upSet("run", "Pakej disahkan, memasang...")
		go doUploadUpdate(ver, vp, vpro)
		jr(w, map[string]any{"ok": true, "msg": "Pakej disahkan: versi " + ver})
	})
	mux.HandleFunc("/api/update/rollback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			jr(w, map[string]any{"ok": false, "msg": "Guna POST"})
			return
		}
		if !hasRollback() {
			jr(w, map[string]any{"ok": false, "msg": "Tiada versi sebelum untuk dipulihkan"})
			return
		}
		upMu.Lock()
		busy := upRun
		if !busy {
			upRun = true
		}
		upMu.Unlock()
		if busy {
			jr(w, map[string]any{"ok": false, "msg": "Kemas kini sedang berjalan"})
			return
		}
		upSet("run", "Memulakan pemulihan...")
		go doRollback()
		jr(w, map[string]any{"ok": true, "msg": "Pemulihan dimulakan"})
	})
	mux.HandleFunc("/api/update/state", func(w http.ResponseWriter, r *http.Request) {
		jr(w, upGet())
	})
	act("/api/update/apply", func(r *http.Request) (string, bool) {
		upMu.Lock()
		if upRun {
			upMu.Unlock()
			return "Kemas kini sedang berjalan", true
		}
		upRun = true
		upMu.Unlock()
		upSet("run", "Memulakan...")
		go doUpdate(strings.TrimSpace(string(body(r, 16))) == "force")
		return "Kemas kini dimulakan", true
	})
	regOpts(mux, act, body)
	regRoutes(mux, act, body)
	regSQM(mux, act, body)
	regFeat(mux, act, body)
	regWan(mux, act)
	regAd(mux, act, body)
	act("/api/mode", func(r *http.Request) (string, bool) {
		switch strings.TrimSpace(string(body(r, 16))) {
		case "vless":
			if len(loadNodes()) == 0 {
				return "Tambah profil dahulu", false
			}
			runVP(true, "to-vless")
			return "Menyambung VLESS Pro...", true
		case "xlite":
			runVP(true, "to-xlite")
			return "Kembali ke XLITE...", true
		}
		return "Mod tidak sah", false
	})
	authed := func(r *http.Request) bool {
		passMu.Lock()
		p := pass
		passMu.Unlock()
		if c, err := r.Cookie("vp"); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(tokenOf(p))) == 1 {
			return true
		}
		if _, pw, ok := r.BasicAuth(); ok && subtle.ConstantTimeCompare([]byte(pw), []byte(p)) == 1 {
			return true
		}
		return false
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/login":
			pw := strings.TrimSpace(string(body(r, 256)))
			passMu.Lock()
			p := pass
			passMu.Unlock()
			if subtle.ConstantTimeCompare([]byte(pw), []byte(p)) != 1 {
				time.Sleep(time.Second)
				w.WriteHeader(401)
				jr(w, map[string]any{"ok": false})
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "vp", Value: tokenOf(p), Path: "/", HttpOnly: true, MaxAge: 86400 * 30})
			jr(w, map[string]any{"ok": true})
			return
		case "/api/logout":
			http.SetCookie(w, &http.Cookie{Name: "vp", Value: "", Path: "/", MaxAge: -1})
			jr(w, map[string]any{"ok": true})
			return
		case "/":
			mux.ServeHTTP(w, r)
			return
		}
		if !authed(r) {
			w.WriteHeader(401)
			jr(w, map[string]any{"auth": false})
			return
		}
		if mustChange() && r.URL.Path != "/api/pass" && r.URL.Path != "/api/status" {
			w.WriteHeader(403)
			jr(w, map[string]any{"ok": false, "mustchange": true, "msg": "Tukar kata laluan dahulu"})
			return
		}
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Addr: listen, Handler: h, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second}
	fmt.Fprintln(os.Stderr, "mendengar", listen)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
