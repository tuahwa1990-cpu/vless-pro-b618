package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

func mk(t *testing.T) (string, ed25519.PrivateKey, []byte) {
	pk, sk, _ := ed25519.GenerateKey(rand.Reader)
	kn := []byte("12345678")
	pb := append(append([]byte("Ed"), kn...), pk...)
	return base64.StdEncoding.EncodeToString(pb), sk, kn
}
func sign(sk ed25519.PrivateKey, kn []byte, msg string) string {
	s := ed25519.Sign(sk, []byte(msg))
	return base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), kn...), s...))
}
func TestLic(t *testing.T) {
	os.Setenv("VP_MAC", "AA:BB:CC:DD:EE:FF")
	home = t.TempDir()
	nowFn = func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFn = time.Now })
	pub, sk, kn := mk(t)
	pubkey = pub
	id := devID20()
	cases := map[string]string{
		id + ":Ali:20261108:0":                "active",
		id + ":Ali:20261108:20200101":         "expired",
		"AAAAAAAAAAAAAAAAAAAA:Ali:20261108:0": "wrongdevice",
	}
	for pay, want := range cases {
		for _, nl := range []string{"", "\n"} {
			k := pay + "." + sign(sk, kn, pay+nl)
			if got := checkKey(k).State; got != want {
				t.Errorf("%s nl=%q got %s want %s", pay, nl, got, want)
			}
		}
	}
	good := id + ":Ali:20261108:0." + sign(sk, kn, id+":Ali:20261108:0")
	if checkKey(strings.Replace(good, "Ali", "Bob", 1)).State != "invalid" {
		t.Error("tamper not invalid")
	}
	if checkKey("").State != "none" || checkKey("abc").State != "invalid" {
		t.Error("none/invalid")
	}
	_, sk2, kn2 := mk(t)
	if checkKey(id+":Ali:20261108:0."+sign(sk2, kn2, id+":Ali:20261108:0")).State != "invalid" {
		t.Error("wrong signer accepted")
	}
}
func TestParse(t *testing.T) {
	p, err := parseLink("vless://11111111-2222-3333-4444-555555555555@example.com:443?type=ws&security=tls&sni=example.com&host=example.com&path=%2Fws&fp=chrome#Nod%20Satu")
	if err != nil || p.Name != "Nod Satu" || p.Net != "ws" || p.Path != "/ws" || p.Port != 443 {
		t.Fatalf("%v %+v", err, p)
	}
	if _, err := parseLink("vmess://x"); err == nil {
		t.Error("vmess accepted")
	}
	r, err := parseLink("vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?security=reality&pbk=abc&sid=01&sni=x.com&flow=xtls-rprx-vision&type=tcp#R")
	if err != nil || r.Flow == "" {
		t.Fatal(err)
	}
	cfg := buildConfig([]*Parsed{r}, nil)
	if cfg["inbounds"] == nil {
		t.Error("cfg")
	}
}

const kunciUji = "4ADEA81F987A52934E2B:Pelanggan:20261117:0.RWQEaRIHBlatsy6+glxHF1cp4Ljg4P9gaGuPYpqfPqxZufhp5MnkKClPUQ5BGsEd5Ph1P8XW5i+1MHFJPLRf8X/3U6wGKVZdiQY="

func fixClock(t *testing.T, tm time.Time) {
	t.Helper()
	nowFn = func() time.Time { return tm }
	t.Cleanup(func() { nowFn = time.Now })
}

func TestSekat(t *testing.T) {
	h := sha256.Sum256([]byte(kunciUji))
	if hex.EncodeToString(h[:]) != "d6b21bae183cf9ff96a01a5aefb0dc027694b1acbd935cf2e7f1812c9cc2a79b" {
		t.Fatal("hash kunci tidak sepadan dengan senarai sekat")
	}
	for name, k := range map[string]string{
		"tepat":        kunciUji,
		"ruang hujung": "  " + kunciUji + " \n",
		"berbaris":     kunciUji[:30] + "\n" + kunciUji[30:60] + " " + kunciUji[60:],
	} {
		if !kunciDisekat(k) {
			t.Errorf("%s: tidak disekat", name)
		}
		if got := checkKey(k).State; got != "revoked" {
			t.Errorf("%s: checkKey=%s", name, got)
		}
	}
	if kunciDisekat(kunciUji+"x") || kunciDisekat("") || kunciDisekat("abc") {
		t.Error("kunci lain disekat")
	}
}

func TestSekatTersimpanDanAktifkan(t *testing.T) {
	home = t.TempDir()
	_ = os.MkdirAll(home+"/etc", 0700)
	fixClock(t, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	// aktifkan kunci disekat: ditolak, tiada fail ditulis
	if l := activateKey(kunciUji); l.State != "revoked" {
		t.Fatalf("aktifkan: %s", l.State)
	}
	if _, err := os.Stat(home + "/etc/license"); err == nil {
		t.Fatal("kunci disekat disimpan")
	}
	// lesen tersimpan yang disekat: dibuang, penanda ditulis, keadaan kekal revoked
	_ = os.WriteFile(home+"/etc/license", []byte(kunciUji+"\n"), 0600)
	if l := readLic(); l.State != "revoked" || !locked(l) {
		t.Fatalf("startup: %s", l.State)
	}
	if _, err := os.Stat(home + "/etc/license"); err == nil {
		t.Fatal("lesen tersimpan tidak dibuang")
	}
	if l := readLic(); l.State != "revoked" {
		t.Fatalf("selepas buang: %s", l.State)
	}
	// kunci sah baharu mengaktifkan semula dan membuang penanda
	os.Setenv("VP_MAC", "AA:BB:CC:DD:EE:FF")
	pub, sk, kn := mk(t)
	pubkey = pub
	pay := devID20() + ":Baru:20261119:0"
	if l := activateKey(pay + "." + sign(sk, kn, pay)); l.State != "active" {
		t.Fatalf("kunci baharu: %s", l.State)
	}
	if l := readLic(); l.State != "active" || locked(l) {
		t.Fatalf("selepas aktif: %s", l.State)
	}
}

func TestTarikhTamat(t *testing.T) {
	home = t.TempDir()
	os.Setenv("VP_MAC", "AA:BB:CC:DD:EE:FF")
	pub, sk, kn := mk(t)
	pubkey = pub
	id := devID20()
	key := func(exp string) string {
		p := id + ":Ali:20261101:" + exp
		return p + "." + sign(sk, kn, p)
	}
	d := func(y int, m time.Month, dd, hh int) time.Time { return time.Date(y, m, dd, hh, 0, 0, 0, time.UTC) }

	fixClock(t, d(2026, 11, 19, 12)) // 20261118 sudah luput
	if s := checkKey(key("20261118")).State; s != "expired" {
		t.Errorf("exp semalam: %s", s)
	}
	if l := activateKey(key("20261118")); l.State != "expired" {
		t.Errorf("aktifkan luput: %s", l.State)
	}
	if _, err := os.Stat(home + "/etc/license"); err == nil {
		t.Error("lesen luput disimpan")
	}
	if s := checkKey(key("20261119")).State; s != "active" {
		t.Errorf("exp hari ini: %s", s)
	}
	if s := checkKey(key("20271231")).State; s != "active" {
		t.Errorf("exp depan: %s", s)
	}
	for _, e := range []string{"1", "00000000", "20260101"} {
		if s := checkKey(key(e)).State; s != "expired" {
			t.Errorf("exp %s: %s", e, s)
		}
	}
	if locked(checkKey(key("20261118"))) != true {
		t.Error("luput mesti dikunci")
	}

	// semakan semula lesen tersimpan: aktif hari ini, luput esok
	_ = activateKey(key("20261119"))
	if s := readLic().State; s != "active" {
		t.Errorf("tersimpan hari ini: %s", s)
	}
	fixClock(t, d(2026, 11, 20, 1))
	if s := readLic().State; s != "expired" {
		t.Errorf("tersimpan selepas luput: %s", s)
	}
	if _, err := os.Stat(home + "/etc/license"); err != nil {
		t.Error("lesen luput tidak patut dipadam")
	}
}

func TestJamSalahDanUndur(t *testing.T) {
	home = t.TempDir()
	_ = os.MkdirAll(home+"/etc", 0700)
	os.Setenv("VP_MAC", "AA:BB:CC:DD:EE:FF")
	pub, sk, kn := mk(t)
	pubkey = pub
	id := devID20()
	mkKey := func(exp string) string {
		p := id + ":Ali:20261101:" + exp
		return p + "." + sign(sk, kn, p)
	}
	d := func(m time.Month, dd, hh int) time.Time { return time.Date(2026, m, dd, hh, 0, 0, 0, time.UTC) }

	fixClock(t, d(11, 19, 12))
	_ = activateKey(mkKey("20271231"))
	_ = activateKey(mkKey("0")) // lifetime menggantikan; simpan semula bertempoh selepas ini
	_ = activateKey(mkKey("20271231"))
	touchSeen()
	if lastSeen().IsZero() {
		t.Fatal("lastseen tidak ditulis")
	}

	// jam salah semasa boot (1970 / sebelum tarikh binaan): bertempoh ditolak, lesen tidak dipadam
	fixClock(t, time.Unix(86400, 0))
	if s := readLic().State; s != "clock" {
		t.Errorf("jam 1970: %s", s)
	}
	if !locked(readLic()) {
		t.Error("clock mesti dikunci")
	}
	if _, err := os.Stat(home + "/etc/license"); err != nil {
		t.Error("lesen dipadam semasa jam salah")
	}
	// lifetime tidak terjejas oleh jam salah
	if s := checkKey(mkKey("0")).State; s != "active" {
		t.Errorf("lifetime jam salah: %s", s)
	}
	// jam betul semula: aktif kembali tanpa aktifkan semula
	fixClock(t, d(11, 19, 13))
	if s := readLic().State; s != "active" {
		t.Errorf("jam pulih: %s", s)
	}

	// jam diundur jauh (lebih 24 jam) selepas tarikh binaan: ditolak untuk lesen bertempoh sahaja
	const L = 1795348800 // 2026-11-22 12:00 UTC
	_ = os.WriteFile(home+"/etc/lastseen", []byte("1795348800\n"), 0600)
	fixClock(t, d(11, 19, 20))
	if s := readLic().State; s != "clock" {
		t.Errorf("jam diundur jauh: %s", s)
	}
	if s := checkKey(mkKey("0")).State; s != "active" {
		t.Errorf("lifetime jam diundur: %s", s)
	}
	fixClock(t, time.Unix(L-25*3600, 0))
	if s := checkKey(mkKey("20271231")).State; s != "clock" {
		t.Errorf("undur 25 jam: %s", s)
	}
	// kes B618: lastseen tercatat 7 jam lebih awal kerana jam modem pernah 8 jam terlalu awal, kemudian betul semula
	fixClock(t, time.Unix(L-7*3600-8*60, 0))
	if s := checkKey(mkKey("20271231")).State; s != "active" {
		t.Errorf("jam pulih selepas terlalu awal: %s", s)
	}
	fixClock(t, time.Unix(L-23*3600, 0))
	if s := checkKey(mkKey("20271231")).State; s != "active" {
		t.Errorf("undur 23 jam: %s", s)
	}
	// lastseen tidak boleh diundurkan oleh touchSeen
	seenMu.Lock()
	seenAt = time.Time{}
	seenMu.Unlock()
	touchSeen()
	if lastSeen().Unix() != L {
		t.Errorf("lastseen diundurkan: %v", lastSeen().Unix())
	}
	// ... tetapi bergerak ke hadapan
	fixClock(t, time.Unix(L+3600, 0))
	seenMu.Lock()
	seenAt = time.Time{}
	seenMu.Unlock()
	touchSeen()
	if lastSeen().Unix() != L+3600 {
		t.Errorf("lastseen tidak maju: %v", lastSeen().Unix())
	}
	// touchSeen tidak menulis semasa jam tidak dipercayai
	fixClock(t, time.Unix(86400, 0))
	touchSeen()
	if lastSeen().Unix() != L+3600 {
		t.Errorf("lastseen diubah semasa jam salah: %v", lastSeen())
	}
}
