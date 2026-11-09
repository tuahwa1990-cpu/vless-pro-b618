package main

import (
	"os"
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

func TestAdblockRule(t *testing.T) {
	d := t.TempDir()
	home = d
	xliteDir = d + "/x"
	_ = os.MkdirAll(d+"/etc", 0700)
	_ = os.MkdirAll(d+"/geo", 0700)
	p, _ := parseLink("vless://11111111-1111-1111-1111-111111111111@h.example:443?security=tls&type=ws&path=/&host=h.example#n")
	has := func() bool {
		c := buildConfig([]*Parsed{p}, nil)
		for _, r := range c["routing"].(map[string]any)["rules"].([]any) {
			if m, ok := r.(map[string]any); ok && m["outboundTag"] == "block" {
				return true
			}
		}
		return false
	}
	_ = os.WriteFile(d+"/etc/adblock.json", []byte(`{"on":true}`), 0600)
	if has() {
		t.Fatal("tanpa geosite, peraturan tidak patut dimasukkan")
	}
	_ = os.WriteFile(d+"/geo/geosite.dat", make([]byte, 200000), 0600)
	if !has() {
		t.Fatal("peraturan sekat iklan tiada")
	}
	_ = os.WriteFile(d+"/etc/adblock.json", []byte(`{"on":false}`), 0600)
	if has() {
		t.Fatal("dimatikan")
	}
}
