package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
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

func checkKey(key string) Lic {
	l := Lic{ID: hyph(devID20()), State: "none", Exp: "0"}
	key = strings.Join(strings.Fields(key), "")
	if key == "" {
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
	if len(l.Exp) == 8 && l.Exp != "0" {
		now := time.Now()
		if now.Year() >= 2024 && l.Exp < now.Format("20060102") {
			l.State = "expired"
		}
	}
	return l
}

func readLic() Lic {
	b, _ := os.ReadFile(home + "/etc/license")
	return checkKey(string(b))
}

func locked(l Lic) bool { return l.State != "active" && l.State != "expired" }

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
