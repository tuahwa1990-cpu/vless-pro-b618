package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// pubkey boleh ditanam semasa binaan: -ldflags "-X main.pubkey=BASE64"
var pubkey = ""

type Lic struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Name   string `json:"name"`
	Issued string `json:"issued"`
	Exp    string `json:"exp"`
}

func macAddr() string {
	if m := os.Getenv("VP_MAC"); m != "" {
		return strings.ToLower(m)
	}
	for _, n := range []string{"br0", "br-lan", "eth0", "wl0"} {
		b, err := os.ReadFile("/sys/class/net/" + n + "/address")
		if err == nil {
			s := strings.ToLower(strings.TrimSpace(string(b)))
			if s != "" && s != "00:00:00:00:00:00" {
				return s
			}
		}
	}
	return ""
}

func devID20() string {
	h := sha256.Sum256([]byte("vless:" + macAddr()))
	return strings.ToUpper(hex.EncodeToString(h[:])[:20])
}

func hyph(s string) string {
	var p []string
	for i := 0; i < len(s); i += 4 {
		e := i + 4
		if e > len(s) {
			e = len(s)
		}
		p = append(p, s[i:e])
	}
	return strings.Join(p, "-")
}

var payRe = regexp.MustCompile(`^[0-9A-F]{20}:[A-Za-z0-9_-]{1,24}:[0-9]{8}:[0-9]{1,8}$`)

func pubBytes() []byte {
	s := pubkey
	if b, err := os.ReadFile(home + "/etc/public.key"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			ln = strings.TrimSpace(ln)
			if ln != "" && !strings.HasPrefix(ln, "untrusted comment") {
				s = ln
			}
		}
	}
	if s == "" {
		return nil
	}
	d, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return d
}

// ---------------------------------------------------------------------------
// Senarai sekat kunci.
// Tambah hash SHA-256 (hex, huruf kecil) bagi teks kunci tepat di bawah. Kira hash dengan:
//
//	printf '%s' 'KUNCI' | sha256sum
//
// Simpan hash sahaja, bukan kunci. Binaan baharu mesti diedarkan supaya sekatan berkuat kuasa.
var disekat = map[string]bool{
	"d6b21bae183cf9ff96a01a5aefb0dc027694b1acbd935cf2e7f1812c9cc2a79b": true, // dikeluarkan 20261117, dibatalkan 20261119
	"5d69ae7ceb862029b219e9cf666a48e75570bf39b9df70fa912c3783a02fd646": true, // Pelanggan, dikeluarkan 20261117, dibatalkan 20261123
}

// kunciDisekat: hash teks kunci (selepas TrimSpace) dan juga versi tanpa sebarang ruang putih,
// kerana kunci yang dipetik/disalin berbaris-baris tetap diterima oleh checkKey.
func kunciDisekat(kunci string) bool {
	k := strings.TrimSpace(kunci)
	for _, t := range []string{k, strings.Join(strings.Fields(k), "")} {
		h := sha256.Sum256([]byte(t))
		if disekat[hex.EncodeToString(h[:])] {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Jam. Modem tanpa RTC boleh bermula dengan jam salah sehingga NTP menyegerak.
var nowFn = time.Now

// Waktu Malaysia (UTC+8): lesen bertempoh tamat tepat tengah malam di sini, bukan mengikut zon modem.
var myt = time.FixedZone("MYT", 8*3600)

// Jam sebelum tarikh binaan ini dianggap salah. Lesen bertempoh sahaja ditolak (lesen seumur hidup tidak terjejas).
var buildClock = time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)

const (
	// Had undur yang dibenarkan. B618 pernah mencatat jam 8 jam terlalu awal (masa tempatan dianggap UTC) sebelum
	// disegerak semula; jam yang betul selepas itu bukan jam diundur. Undur lebih besar daripada ini ditolak.
	clockSlack = 24 * time.Hour
	seenEvery  = time.Hour // paling kerap fail lastseen ditulis (jimat flash)
)

func lastSeen() time.Time {
	b, _ := os.ReadFile(home + "/etc/lastseen")
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// clockTrusted: jam tidak sebelum tarikh binaan dan tidak lebih awal (melebihi clockSlack) daripada "masa terakhir dilihat".
func clockTrusted() bool {
	now := nowFn()
	if now.Before(buildClock) {
		return false
	}
	if ls := lastSeen(); !ls.IsZero() && now.Add(clockSlack).Before(ls) {
		return false
	}
	return true
}

var (
	seenMu sync.Mutex
	seenAt time.Time
)

// touchSeen merekod masa terakhir dilihat, hanya jika jam boleh dipercayai.
func touchSeen() {
	if !clockTrusted() {
		return
	}
	now := nowFn()
	seenMu.Lock()
	defer seenMu.Unlock()
	if !seenAt.IsZero() && now.After(seenAt) && now.Sub(seenAt) < seenEvery {
		return
	}
	if ls := lastSeen(); !ls.IsZero() && !now.After(ls) {
		return // lastseen hanya bergerak ke hadapan, jadi undur kecil berulang tidak boleh menyorong had
	}
	_ = os.MkdirAll(home+"/etc", 0700)
	if os.WriteFile(home+"/etc/lastseen", []byte(strconv.FormatInt(now.Unix(), 10)+"\n"), 0600) == nil {
		seenAt = now
	}
}

func checkKey(key string) Lic {
	l := Lic{ID: hyph(devID20()), State: "none", Exp: "0"}
	raw := key
	key = strings.Join(strings.Fields(key), "")
	if key == "" {
		return l
	}
	// Senarai sekat: sebelum semakan tandatangan.
	if kunciDisekat(raw) {
		l.State = "revoked"
		return l
	}
	l.State = "invalid"
	i := strings.LastIndex(key, ".")
	if i < 0 {
		return l
	}
	pay, sg := key[:i], key[i+1:]
	if !payRe.MatchString(pay) {
		return l
	}
	sig, err := base64.StdEncoding.DecodeString(sg)
	pk := pubBytes()
	if err != nil || len(pk) != 42 || len(sig) != 74 || pk[0] != 'E' || pk[1] != 'd' || sig[0] != 'E' || sig[1] != 'd' {
		return l
	}
	if string(pk[2:10]) != string(sig[2:10]) {
		return l
	}
	if !ed25519.Verify(pk[10:], []byte(pay), sig[10:]) && !ed25519.Verify(pk[10:], []byte(pay+"\n"), sig[10:]) {
		return l
	}
	pp := strings.Split(pay, ":")
	l.Name, l.Issued, l.Exp = pp[1], pp[2], pp[3]
	if pp[0] != devID20() {
		l.State = "wrongdevice"
		return l
	}
	l.State = "active"
	if l.Exp != "0" { // lesen bertempoh; hanya "0" tepat ialah seumur hidup
		now := nowFn()
		switch {
		case !clockTrusted():
			l.State = "clock" // jam salah atau diundur: tolak, tetapi jangan padam lesen
		default:
			today, _ := strconv.Atoi(now.In(myt).Format("20060102")) // tarikh ikut waktu Malaysia
			if e, err := strconv.Atoi(l.Exp); err != nil || e < today {
				l.State = "expired"
			}
		}
	}
	return l
}

// readLic membaca lesen tersimpan. Kunci tersimpan yang disekat dibuang dan penanda etc/revoked ditulis
// supaya paparan terus menunjukkan "Lesen dibatalkan" sehingga kunci sah baharu diaktifkan.
func readLic() Lic {
	touchSeen()
	b, err := os.ReadFile(home + "/etc/license")
	if err != nil || strings.TrimSpace(string(b)) == "" {
		if _, e := os.Stat(home + "/etc/revoked"); e == nil {
			l := checkKey("")
			l.State = "revoked"
			return l
		}
	}
	l := checkKey(string(b))
	if l.State == "revoked" {
		_ = os.MkdirAll(home+"/etc", 0700)
		_ = os.WriteFile(home+"/etc/revoked", []byte("1\n"), 0600)
		_ = os.Remove(home + "/etc/license")
	}
	return l
}

// activateKey menyemak dan, jika sah, menyimpan kunci. Kunci yang disekat tidak menyentuh lesen tersimpan lain.
func activateKey(key string) Lic {
	l := checkKey(key)
	if l.State == "active" {
		_ = os.MkdirAll(home+"/etc", 0700)
		if os.WriteFile(home+"/etc/license", []byte(strings.Join(strings.Fields(key), "")+"\n"), 0600) == nil {
			_ = os.Remove(home + "/etc/revoked")
		}
	}
	return l
}

// Hanya "active" membuka fungsi VPN. expired, revoked, clock, wrongdevice, invalid dan none dikunci.
func locked(l Lic) bool { return l.State != "active" }

var licPrev string

// licSweep: semak semula lesen tersimpan (startup dan setiap jam).
// revoked atau expired: sekat fungsi VPN dan putuskan VPN yang sedang berjalan.
// clock: tiada tindakan memutuskan; jika kemudian jam betul dan lesen aktif, pulihkan VPN yang gagal naik semasa boot.
func licSweep() {
	l := readLic()
	touchSeen()
	mode := readStr(home+"/etc/mode", "xlite")
	if (l.State == "revoked" || l.State == "expired") && mode == "vless" {
		fmt.Fprintln(os.Stderr, "lesen "+l.State+": VPN diputuskan")
		runVP(true, "to-xlite")
	}
	if licPrev == "clock" && l.State == "active" && mode == "vless" && !vlessUp() {
		runVP(true, "to-vless")
	}
	licPrev = l.State
}

func licLoop() {
	for {
		licSweep()
		d := time.Hour
		if licPrev == "clock" {
			d = 2 * time.Minute
		}
		time.Sleep(d)
	}
}

func pubFP() string {
	var s string
	b := pubBytes()
	if b == nil {
		return "tiada"
	}
	h := sha256.Sum256([]byte(base64.StdEncoding.EncodeToString(b) + "\n"))
	s = hex.EncodeToString(h[:])[:12]
	return s
}
