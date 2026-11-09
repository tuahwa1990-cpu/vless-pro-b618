package main

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPickFailover(t *testing.T) {
	// profil semasa boleh dicapai: tiada penukaran
	if g := pickFailover([]int{20, 10, 30}, 0, false); g != -1 {
		t.Fatal(g)
	}
	// profil semasa mati: pilih ping terendah
	if g := pickFailover([]int{-1, 50, 10}, 0, false); g != 2 {
		t.Fatal(g)
	}
	// semua mati
	if g := pickFailover([]int{-1, -1}, 0, false); g != -1 {
		t.Fatal(g)
	}
	// putaran: seterusnya yang boleh dicapai, melangkau yang mati
	if g := pickFailover([]int{5, -1, 7}, 0, true); g != 2 {
		t.Fatal(g)
	}
	if g := pickFailover([]int{5, 6, 7}, 2, true); g != 0 {
		t.Fatal(g)
	}
	// hanya satu profil
	if g := pickFailover([]int{5}, 0, true); g != -1 {
		t.Fatal(g)
	}
}

func TestPeriod(t *testing.T) {
	loc := time.UTC
	q := Quota{Per: "month", RDay: 15}
	got := periodStart(time.Date(2026, 11, 9, 3, 0, 0, 0, loc), q)
	if !got.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, loc)) {
		t.Fatal(got)
	}
	got = periodStart(time.Date(2026, 11, 20, 3, 0, 0, 0, loc), q)
	if !got.Equal(time.Date(2026, 11, 15, 0, 0, 0, 0, loc)) {
		t.Fatal(got)
	}
	got = periodStart(time.Date(2026, 1, 3, 0, 0, 0, 0, loc), Quota{Per: "month", RDay: 10})
	if !got.Equal(time.Date(2025, 12, 10, 0, 0, 0, 0, loc)) {
		t.Fatal(got)
	}
	d := Quota{Per: "day"}
	st := periodStart(time.Date(2026, 11, 9, 23, 59, 0, 0, loc), d)
	if !st.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, loc)) || !periodEnd(st, d).Equal(time.Date(2026, 11, 10, 0, 0, 0, 0, loc)) {
		t.Fatal(st)
	}
}

func TestQuotaState(t *testing.T) {
	qCfg = Quota{On: true, Limit: 10, Per: "month", RDay: 1}
	qSt = qState{Rx: 5e9}
	if quotaState() != "ok" {
		t.Fatal(quotaState())
	}
	qSt = qState{Rx: 4e9, Tx: 4.5e9}
	if quotaState() != "warn" {
		t.Fatal(quotaState())
	}
	qSt = qState{Rx: 10e9}
	if quotaState() != "full" {
		t.Fatal(quotaState())
	}
	qCfg.On = false
	if quotaState() != "off" {
		t.Fatal(quotaState())
	}
	if n := normQuota(Quota{Per: "x", RDay: 40, Limit: -3}); n.Per != "month" || n.RDay != 1 || n.Limit != 0 {
		t.Fatal(n)
	}
}

func TestWanMsg(t *testing.T) {
	if wanMsg("1.1.1.1", "2.2.2.2") == wanMsg("1.1.1.1", "1.1.1.1") || wanMsg("", "") == "" {
		t.Fatal("mesej")
	}
}

func TestParseAdLine(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0 ads.example.com":         "ads.example.com",
		"127.0.0.1 Tracker.Foo.net # c":   "tracker.foo.net",
		"||doubleclick.net^":              "doubleclick.net",
		"||ads.x.com^$important":          "ads.x.com",
		"||x.com^$third-party":            "x.com",
		"||x.com/path^":                   "",
		"@@||allowed.com^":                "",
		"! komen":                         "",
		"# komen":                         "",
		"0.0.0.0 localhost":               "",
		"127.0.0.1 localhost.localdomain": "localhost.localdomain",
		"||*.ads.com^":                    "",
		"":                                "",
	}
	for in, want := range cases {
		if got := parseAdLine(in); got != want {
			t.Errorf("%q: %q != %q", in, got, want)
		}
	}
}

func TestBlockerMatch(t *testing.T) {
	b := &blocker{set: map[uint64]struct{}{h64("ads.com"): {}, h64("tracker.example.org"): {}}, white: map[uint64]struct{}{h64("good.ads.com"): {}}}
	for n, want := range map[string]bool{
		"ads.com": true, "x.y.ads.com": true, "good.ads.com": false, "a.good.ads.com": false,
		"example.org": false, "tracker.example.org": true, "sub.tracker.example.org": true,
		"com": false, "notads.com": false,
	} {
		if b.match(n) != want {
			t.Errorf("%s: harap %v", n, want)
		}
	}
}

func TestDNSBlockedReply(t *testing.T) {
	q := []byte{0xab, 0xcd, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 3, 'a', 'd', 's', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
	name, qt, qend, ok := dnsQuestion(q)
	if !ok || name != "ads.com" || qt != 1 || qend != len(q) {
		t.Fatal(name, qt, qend, ok)
	}
	r := dnsBlockedReply(q, qt, qend)
	if r[0] != 0xab || r[2]&0x80 == 0 || r[2]&0x01 == 0 || r[3]&0x0f != 0 || r[7] != 1 || len(r) != qend+16 {
		t.Fatalf("%x", r)
	}
	// AAAA -> 16 bait
	q[len(q)-3] = 28
	_, qt, qend, _ = dnsQuestion(q)
	if r := dnsBlockedReply(q, qt, qend); len(r) != qend+28 {
		t.Fatal(len(r))
	}
	// jenis lain -> NODATA
	q[len(q)-3] = 16
	_, qt, qend, _ = dnsQuestion(q)
	if r := dnsBlockedReply(q, qt, qend); len(r) != qend || r[7] != 0 {
		t.Fatal("nodata")
	}
	// bukan soalan / rosak
	if _, _, _, ok := dnsQuestion(q[:10]); ok {
		t.Fatal("pendek")
	}
}

func TestNormAd(t *testing.T) {
	c := normAd(AdCfg{Lists: []string{"x", "stevenblack"}, White: []string{" *.Good.com ", "bad domain", "nodot"}, Black: []string{"A.com."}})
	if len(c.Lists) != 1 || c.Lists[0] != "stevenblack" || len(c.White) != 1 || c.White[0] != "good.com" || c.Black[0] != "a.com" {
		t.Fatal(c)
	}
}

func TestAdAnswerForward(t *testing.T) {
	home = t.TempDir()
	_ = os.MkdirAll(home+"/etc", 0700)
	up, err := net.ListenPacket("udp", "127.0.0.1:53")
	if err != nil {
		t.Skip("tidak boleh bind 127.0.0.1:53")
	}
	defer up.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			n, a, err := up.ReadFrom(buf)
			if err != nil {
				return
			}
			r := append([]byte{}, buf[:n]...)
			r[2] |= 0x80
			r[11] = 0xEE // penanda jawapan hulu
			_, _ = up.WriteTo(r, a)
		}
	}()
	adBlk = &blocker{set: map[uint64]struct{}{h64("ads.com"): {}}, white: map[uint64]struct{}{}}
	mk := func(d string) []byte {
		q := []byte{1, 2, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
		for _, p := range strings.Split(d, ".") {
			q = append(q, byte(len(p)))
			q = append(q, p...)
		}
		return append(q, 0, 0, 1, 0, 1)
	}
	b := adAnswer(mk("x.ads.com"))
	if b == nil || b[7] != 1 || b[11] == 0xEE {
		t.Fatalf("patut disekat: %x", b)
	}
	f := adAnswer(mk("good.org"))
	if f == nil || f[11] != 0xEE {
		t.Fatalf("patut dihantar: %x", f)
	}
	stMu.Lock()
	defer stMu.Unlock()
	if stData.AllQ != 2 || stData.AllB != 1 || stData.Top["x.ads.com"] != 1 {
		t.Fatal(stData)
	}
}
