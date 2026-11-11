package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ===================== Failover profil =====================

type FOEv struct {
	T    int64  `json:"t"`
	From string `json:"from"`
	To   string `json:"to"`
	Why  string `json:"why"`
}

type FOCfg struct {
	On bool `json:"on"`
}

var (
	foMu   sync.Mutex
	foLast time.Time
	foBad  = map[string]time.Time{} // profil yang baru gagal (kunci: pautan), dilangkau sementara
)

const foBadTTL = 10 * time.Minute

// maskBad menandakan profil yang baru gagal sebagai tidak boleh dicapai (-1), kecuali profil semasa.
func maskBad(ns []Node, pings []int, cur int) {
	for i := range ns {
		if t, ok := foBad[ns[i].Link]; ok && time.Since(t) < foBadTTL && i != cur && i < len(pings) {
			pings[i] = -1
		}
	}
}

func loadFO() FOCfg {
	var c FOCfg
	if b, err := os.ReadFile(home + "/etc/failover.json"); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func loadFOLog() []FOEv {
	var l []FOEv
	if b, err := os.ReadFile(home + "/etc/failover_log.json"); err == nil {
		_ = json.Unmarshal(b, &l)
	}
	return l
}

func addFOLog(e FOEv) {
	l := append(loadFOLog(), e)
	if len(l) > 20 {
		l = l[len(l)-20:]
	}
	b, _ := json.Marshal(l)
	_ = os.WriteFile(home+"/etc/failover_log.json", b, 0600)
}

// pickFailover memilih indeks profil baharu berdasarkan ping TCP (-1 = tidak boleh dicapai).
// Mengembalikan -1 jika tiada penukaran yang wajar.
// rotate: kegagalan berulang walaupun pelayan boleh dicapai, jadi paksa tukar ke profil seterusnya.
func pickFailover(pings []int, cur int, rotate bool) int {
	if cur >= 0 && cur < len(pings) && pings[cur] >= 0 && !rotate {
		return -1
	}
	if rotate {
		for k := 1; k < len(pings); k++ {
			i := (cur + k + len(pings)) % len(pings)
			if i != cur && pings[i] >= 0 {
				return i
			}
		}
		return -1
	}
	best := -1
	for i, p := range pings {
		if i != cur && p >= 0 && (best < 0 || p < pings[best]) {
			best = i
		}
	}
	return best
}

// pickAlt: dipanggil bila sambungan sah gagal walaupun TCP ke pelayan semasa masih boleh dicapai.
// Pilih profil lain yang boleh dicapai dengan ping terendah.
func pickAlt(pings []int, cur int) int {
	best := -1
	for i, p := range pings {
		if i != cur && p >= 0 && (best < 0 || p < pings[best]) {
			best = i
		}
	}
	return best
}

// wanDelta: bait diterima pada antara muka WAN sejak semakan pemantau sebelumnya (kira-kira 30 saat).
var wanDelta atomic.Uint64

// trafficFlowing benar jika data masuk yang ketara (>= 64 KB sejak semakan lalu) bukti nod berfungsi.
func trafficFlowing() bool {
	if statsOK.Load() {
		return nodeDelta.Load() >= 32<<10 // bait sebenar melalui nod (statistik xray)
	}
	return wanDelta.Load() >= 64<<10
}

// trafficWeak: ada sedikit data masuk (4-64 KB) - boleh jadi trafik latar modem, bukan bukti nod hidup.
// Dengan statistik xray, trafik latar sudah ditapis, jadi tidak perlu menunggu lebih lama.
func trafficWeak() bool { return !statsOK.Load() && wanDelta.Load() >= 4<<10 }

// nodeDelta: bait turun yang benar-benar melalui nod proksi (bukan direct) sejak semakan lalu.
var nodeDelta atomic.Uint64
var statsOK atomic.Bool
var lastNodeDown uint64

// nodeDown membaca jumlah bait turun semua outbound proksi daripada API statistik xray.
func nodeDown() (uint64, bool) {
	xb := ""
	for _, c := range []string{home + "/bin/xray", xliteDir + "/bin/xray", "/usr/bin/xray"} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			xb = c
			break
		}
	}
	if xb == "" {
		return 0, false
	}
	out := runOut(4*time.Second, xb, "api", "statsquery", "-s", "127.0.0.1:10856", "-pattern", "outbound>>>")
	return parseNodeDown(out)
}

// parseNodeDown: jumlah bait turun semua outbound selain direct/block. Nilai kosong (0) tiada dalam JSON.
func parseNodeDown(out string) (uint64, bool) {
	var r struct {
		Stat []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"stat"`
	}
	i := strings.Index(out, "{")
	if i < 0 || json.Unmarshal([]byte(out[i:]), &r) != nil {
		return 0, false
	}
	var tot uint64
	for _, e := range r.Stat {
		f := strings.Split(e.Name, ">>>")
		if len(f) != 4 || f[0] != "outbound" || f[3] != "downlink" || f[1] == "direct" || f[1] == "block" {
			continue
		}
		switch v := e.Value.(type) {
		case string:
			n, _ := strconv.ParseUint(v, 10, 64)
			tot += n
		case float64:
			tot += uint64(v)
		}
	}
	return tot, true
}

func statsTick() {
	if !vlessUp() {
		statsOK.Store(false)
		return
	}
	t, ok := nodeDown()
	if !ok {
		statsOK.Store(false)
		return
	}
	if statsOK.Load() && t >= lastNodeDown {
		nodeDelta.Store(t - lastNodeDown)
	} else {
		nodeDelta.Store(0)
	}
	lastNodeDown = t
	statsOK.Store(true)
}

// lanHist: bait diterima daripada peranti LAN/WiFi dalam 3 semakan terakhir (kira-kira 90 saat).
var lanHist [3]atomic.Uint64

func lanPush(d uint64) {
	lanHist[0].Store(lanHist[1].Load())
	lanHist[1].Store(lanHist[2].Load())
	lanHist[2].Store(d)
}

// userDemand benar jika ada peranti pengguna yang aktif menghantar data (>= 2 KB dalam 90 saat terakhir).
// Tanpa pengguna, nod tidak perlu ditukar walaupun ujian dalaman gagal.
func userDemand() bool {
	for i := range lanHist {
		if lanHist[i].Load() >= 2<<10 {
			return true
		}
	}
	// peranti masih tersambung (WiFi/LAN) walaupun telefon beralih ke data sendiri bila WiFi tiada internet
	return lanClients() > 0
}

// lanClients mengira peranti dalam jadual ARP pada antara muka LAN (tanpa pintu masuk modem sendiri).
func lanClients() int {
	b, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return 0
	}
	dev := lanDev()
	n := 0
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) >= 6 && f[5] == dev && f[2] != "0x0" {
			n++
		}
	}
	return n
}

var foFails atomic.Int32 // kegagalan berturut dikesan pemantau (0-3)

func pingAll(ns []Node) []int {
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
			res[i] = tcpPingT(h, pt, 10*time.Second) // pautan perlahan (RTT 3 s+) jangan dikira mati
			<-sem
		}(i, p.Host, p.Port)
	}
	wg.Wait()
	return res
}

// failoverSwitch cuba tukar profil. Benar jika profil ditukar (dan xray dimulakan semula).
func failoverSwitch() bool {
	foMu.Lock()
	defer foMu.Unlock()
	ns := loadNodes()
	if len(ns) < 2 {
		return false
	}
	cur := selIdx()
	pings := pingAll(ns)
	rotate := !foLast.IsZero() && time.Since(foLast) < 15*time.Minute
	// profil semasa baru disahkan gagal: ingat supaya tidak dicuba semula dalam beberapa minit
	if cur >= 0 && cur < len(ns) {
		foBad[ns[cur].Link] = time.Now()
	}
	raw := append([]int(nil), pings...)
	maskBad(ns, pings, cur)
	n := pickFailover(pings, cur, rotate)
	if n < 0 {
		n = pickAlt(pings, cur) // pemantau sudah mengesahkan sambungan gagal
	}
	if n < 0 && len(foBad) > 0 {
		// semua calon pernah gagal baru-baru ini: lupakan ingatan dan cuba semula daripada awal
		foBad = map[string]time.Time{}
		if cur >= 0 && cur < len(ns) {
			foBad[ns[cur].Link] = time.Now()
		}
		pings = raw
		n = pickFailover(pings, cur, rotate)
		if n < 0 {
			n = pickAlt(pings, cur)
		}
	}
	if n < 0 {
		return false
	}
	why := "pelayan tidak dapat dicapai"
	if rotate && cur >= 0 && cur < len(pings) && pings[cur] >= 0 {
		why = "sambungan gagal berulang"
	}
	from := ""
	if cur >= 0 && cur < len(ns) {
		from = ns[cur].Name
	}
	_ = setSel(n)
	setSelGroup(-1)
	foLast = time.Now()
	addFOLog(FOEv{T: time.Now().Unix(), From: from, To: ns[n].Name, Why: why})
	fmt.Fprintln(os.Stderr, "failover:", from, "->", ns[n].Name, why)
	runVPWait(90*time.Second, "restart")
	return true
}

// recoverVP: dipanggil oleh pemantau bila sambungan gagal.
// aliveVia benar jika mana-mana URL semakan berjaya melalui nod (lebih sabar daripada satu URL sahaja).
func aliveVia() bool {
	if trafficFlowing() {
		return true // data sebenar sedang mengalir: nod berfungsi walaupun ujian gagal
	}
	urls := []string{loadOpts().PingURL, "http://www.youtube.com/generate_204", "http://cp.cloudflare.com/generate_204", "http://connectivitycheck.gstatic.com/generate_204"}
	seen := map[string]bool{}
	for _, u := range urls {
		if seen[u] {
			continue
		}
		seen[u] = true
		if proxyOK(u, 8*time.Second) {
			return true
		}
	}
	return false
}

// verifyNew mengesahkan nod yang baru dipilih (~40 saat): data sebenar melalui nod (statistik xray) dianggap lulus;
// jika tiada trafik, URL ujian pengguna (PingURL) mesti berjaya. Nod yang hanya lulus ujian IP tetapi tidak
// membawa trafik sebenar (cth. YouTube tidak boleh dibuka) ditinggalkan dengan cepat.
func verifyNew() bool {
	u := loadOpts().PingURL
	base, bok := nodeDown()
	for a := 0; a < 3; a++ {
		time.Sleep(5 * time.Second)
		if cur, ok := nodeDown(); bok && ok && cur >= base && cur-base >= 16<<10 {
			return true
		}
		if proxyOK(u, 8*time.Second) {
			return true
		}
	}
	return false
}

// recoverVP: dipanggil oleh pemantau bila sambungan gagal. Benar jika profil ditukar.
func recoverVP() bool {
	// pastikan benar-benar rosak: jangan tukar nod yang sebenarnya masih berfungsi
	if vlessUp() && (aliveVia() || !userDemand()) {
		return false
	}
	if loadFO().On && selGroup() < 0 && failoverSwitch() {
		// sahkan internet pada profil baharu; restart sudah selesai (ditunggu), beri masa Xray bersambung
		n := len(loadNodes())
		for k := 0; k < n; k++ {
			if verifyNew() || !failoverSwitch() {
				break
			}
		}
		return true
	}
	runVPWait(90*time.Second, "restart")
	return false
}

// ===================== Kuota data =====================

type Quota struct {
	On    bool    `json:"on"`
	Limit float64 `json:"limit"`  // GB
	Per   string  `json:"period"` // month | day
	RDay  int     `json:"rday"`   // hari reset bulanan 1-28
	Cut   bool    `json:"cut"`    // sekat internet bila habis
}

type qState struct {
	Start   int64  `json:"start"`
	Rx      uint64 `json:"rx"`
	Tx      uint64 `json:"tx"`
	Blocked bool   `json:"blocked"`
}

var (
	qMu    sync.Mutex
	qCfg   Quota
	qSt    qState
	qDirty bool
	qIface string // antara muka yang dipasang sekatan
)

func normQuota(q Quota) Quota {
	if q.Per != "day" {
		q.Per = "month"
	}
	if q.RDay < 1 || q.RDay > 28 {
		q.RDay = 1
	}
	if q.Limit < 0 || q.Limit > 100000 {
		q.Limit = 0
	}
	return q
}

func loadQuota() Quota {
	var q Quota
	if b, err := os.ReadFile(home + "/etc/quota.json"); err == nil {
		_ = json.Unmarshal(b, &q)
	}
	return normQuota(q)
}

func saveQuotaCfg(q Quota) error {
	b, _ := json.Marshal(q)
	return os.WriteFile(home+"/etc/quota.json", b, 0600)
}

func saveQState() {
	b, _ := json.Marshal(qSt)
	_ = os.WriteFile(home+"/etc/quota_state.json", b, 0600)
	qDirty = false
}

// periodStart: permulaan tempoh semasa (waktu tempatan).
func periodStart(now time.Time, q Quota) time.Time {
	y, m, d := now.Date()
	loc := now.Location()
	if q.Per == "day" {
		return time.Date(y, m, d, 0, 0, 0, 0, loc)
	}
	if d >= q.RDay {
		return time.Date(y, m, q.RDay, 0, 0, 0, 0, loc)
	}
	return time.Date(y, m-1, q.RDay, 0, 0, 0, 0, loc)
}

func periodEnd(start time.Time, q Quota) time.Time {
	if q.Per == "day" {
		return start.AddDate(0, 0, 1)
	}
	return start.AddDate(0, 1, 0)
}

// quotaRoll: mulakan semula pengiraan jika tempoh bertukar. Panggil dengan qMu dikunci.
func quotaRoll(now time.Time) {
	if now.Year() < 2024 {
		return
	}
	ps := periodStart(now, qCfg).Unix()
	if qSt.Start != ps {
		qSt = qState{Start: ps}
		qDirty = true
	}
}

func quotaAdd(dr, dt uint64) {
	qMu.Lock()
	qSt.Rx += dr
	qSt.Tx += dt
	qDirty = true
	qMu.Unlock()
}

func quotaInit() {
	qMu.Lock()
	qCfg = loadQuota()
	if b, err := os.ReadFile(home + "/etc/quota_state.json"); err == nil {
		_ = json.Unmarshal(b, &qSt)
	}
	quotaRoll(time.Now())
	qMu.Unlock()
}

func quotaUsed() float64 { return float64(qSt.Rx+qSt.Tx) / 1e9 }

func quotaState() string {
	if !qCfg.On || qCfg.Limit <= 0 {
		return "off"
	}
	p := quotaUsed() / qCfg.Limit
	switch {
	case p >= 1:
		return "full"
	case p >= 0.8:
		return "warn"
	}
	return "ok"
}

func quotaBlock(on bool) {
	if on == (qIface != "") {
		return
	}
	if on {
		dev := wanIface()
		if dev == "" {
			return
		}
		_, _ = run2("iptables", "-N", "VPQUOTA")
		_, _ = run2("iptables", "-F", "VPQUOTA")
		_, _ = run2("iptables", "-A", "VPQUOTA", "-j", "DROP")
		for _, ch := range []string{"FORWARD", "OUTPUT"} {
			if _, err := run2("iptables", "-C", ch, "-o", dev, "-j", "VPQUOTA"); err != nil {
				_, _ = run2("iptables", "-I", ch, "1", "-o", dev, "-j", "VPQUOTA")
			}
		}
		qIface = dev
		return
	}
	for _, ch := range []string{"FORWARD", "OUTPUT"} {
		for k := 0; k < 3; k++ {
			if _, err := run2("iptables", "-D", ch, "-o", qIface, "-j", "VPQUOTA"); err != nil {
				break
			}
		}
	}
	_, _ = run2("iptables", "-F", "VPQUOTA")
	_, _ = run2("iptables", "-X", "VPQUOTA")
	qIface = ""
}

// quotaEval: kemas keadaan sekatan mengikut penggunaan. Panggil dengan qMu dikunci.
func quotaEval() {
	want := quotaState() == "full" && qCfg.Cut
	if want != qSt.Blocked {
		qSt.Blocked = want
		qDirty = true
	}
	if want && qIface != "" && qIface != wanIface() {
		quotaBlock(false) // antara muka bertukar, pasang semula
	}
	quotaBlock(want)
}

func quotaLoop() {
	quotaInit()
	for {
		time.Sleep(30 * time.Second)
		qMu.Lock()
		quotaRoll(time.Now())
		quotaEval()
		if qDirty {
			saveQState()
		}
		qMu.Unlock()
	}
}

func regFeat(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/failover", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if locked(readLic()) {
				jr(w, map[string]any{"ok": false, "msg": "Lesen belum aktif"})
				return
			}
			var c FOCfg
			if json.Unmarshal(body(r, 256), &c) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Data tidak sah"})
				return
			}
			b, _ := json.Marshal(c)
			_ = os.WriteFile(home+"/etc/failover.json", b, 0600)
			jr(w, map[string]any{"ok": true, "msg": map[bool]string{true: "Failover dihidupkan", false: "Failover dimatikan"}[c.On]})
			return
		}
		ns := loadNodes()
		cur := ""
		if i := selIdx(); i >= 0 && i < len(ns) {
			cur = ns[i].Name
		}
		l := loadFOLog()
		sort.Slice(l, func(i, j int) bool { return l[i].T > l[j].T })
		o := loadOpts()
		jr(w, map[string]any{"cfg": loadFO(), "log": l, "cur": cur, "n": len(ns), "grp": selGroup() >= 0,
			"auto": o.AutoRecon && readStr(home+"/etc/mode", "xlite") == "vless", "fails": foFails.Load()})
	})
	mux.HandleFunc("/api/quota", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if locked(readLic()) {
				jr(w, map[string]any{"ok": false, "msg": "Lesen belum aktif"})
				return
			}
			var q Quota
			if json.Unmarshal(body(r, 512), &q) != nil || q.Limit < 0 || q.Limit > 100000 {
				jr(w, map[string]any{"ok": false, "msg": "Data tidak sah"})
				return
			}
			if q.On && q.Limit < 0.1 {
				jr(w, map[string]any{"ok": false, "msg": "Had minimum 0.1 GB"})
				return
			}
			q = normQuota(q)
			qMu.Lock()
			qCfg = q
			quotaRoll(time.Now())
			if qSt.Start != periodStart(time.Now(), q).Unix() {
				qSt = qState{Start: periodStart(time.Now(), q).Unix()}
			}
			quotaEval()
			saveQState()
			qMu.Unlock()
			if err := saveQuotaCfg(q); err != nil {
				jr(w, map[string]any{"ok": false, "msg": "Gagal simpan: " + err.Error()})
				return
			}
			jr(w, map[string]any{"ok": true, "msg": "Kuota disimpan"})
			return
		}
		qMu.Lock()
		now := time.Now()
		ps := periodStart(now, qCfg)
		out := map[string]any{"cfg": qCfg, "rx": qSt.Rx, "tx": qSt.Tx, "used": quotaUsed(), "state": quotaState(),
			"blocked": qSt.Blocked, "start": ps.Unix(), "end": periodEnd(ps, qCfg).Unix()}
		qMu.Unlock()
		jr(w, out)
	})
	act("/api/quota/reset", func(r *http.Request) (string, bool) {
		qMu.Lock()
		qSt = qState{Start: periodStart(time.Now(), qCfg).Unix()}
		quotaEval()
		saveQState()
		qMu.Unlock()
		return "Pengiraan ditetapkan semula", true
	})
}
