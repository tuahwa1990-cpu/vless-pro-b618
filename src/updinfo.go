package main

// Maklumat kemas kini dalam talian: nota keluaran, tarikh, saiz, semakan berjadual
// dan kemas kini automatik (Beritahu saya / Pasang automatik / Tutup).

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type relNote struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Title   string   `json:"title"`
	Items   []string `json:"items"`
}

type relInfo struct {
	Version string    `json:"version"`
	Date    string    `json:"date"`
	Size    int64     `json:"size"`
	Title   string    `json:"title"`
	Items   []string  `json:"items"`
	History []relNote `json:"history"`
}

type updCache struct {
	At   int64    `json:"at"`
	Ver  string   `json:"ver"`
	Info *relInfo `json:"info"`
	Err  string   `json:"err"`
}

var (
	ckMu sync.Mutex
	ck   updCache
)

func updCacheFile() string { return home + "/etc/updcheck.json" }

// updMode: notify (lalai), auto atau off.
func updMode() string {
	switch m := readStr(home+"/etc/autoupd", "notify"); m {
	case "auto", "off":
		return m
	}
	return "notify"
}

// notesURL: <tag>-latest.txt -> <tag>-notes.json pada pelayan yang sama.
func notesURL() string {
	u := updURL()
	if strings.HasSuffix(u, "-latest.txt") {
		return strings.TrimSuffix(u, "-latest.txt") + "-notes.json"
	}
	return updBase() + "notes.json"
}

func updHost() string {
	if p, err := url.Parse(updURL()); err == nil && p.Host != "" {
		return p.Host
	}
	return updURL()
}

func loadUpdCache() {
	b, err := os.ReadFile(updCacheFile())
	if err != nil {
		return
	}
	var c updCache
	if json.Unmarshal(b, &c) == nil {
		ckMu.Lock()
		ck = c
		ckMu.Unlock()
	}
}

func getUpdCache() updCache {
	ckMu.Lock()
	defer ckMu.Unlock()
	return ck
}

// checkUpdate menyemak versi terbaru dan nota keluaran, kemudian menyimpan hasilnya.
func checkUpdate() updCache {
	c := updCache{At: time.Now().Unix()}
	lv, err := latestVer()
	if err != nil {
		c.Err = "Tidak dapat semak: " + shortErr(err)
		old := getUpdCache()
		c.Ver, c.Info = old.Ver, old.Info
	} else {
		c.Ver = lv
		if b, e := fetch(notesURL(), 64<<10, 15*time.Second); e == nil {
			var ri relInfo
			if json.Unmarshal(b, &ri) == nil && ri.Version != "" {
				c.Info = &ri
			}
		}
	}
	ckMu.Lock()
	ck = c
	ckMu.Unlock()
	if b, e := json.Marshal(c); e == nil {
		_ = os.WriteFile(updCacheFile(), b, 0600)
	}
	return c
}

func updNewer(c updCache) bool { return c.Ver != "" && verCmp(c.Ver, version) > 0 }

// startUpdate memulakan kemas kini dari pelayan (sama seperti butang Kemas kini).
func startUpdate(force bool) bool {
	upMu.Lock()
	if upRun {
		upMu.Unlock()
		return false
	}
	upRun = true
	upMu.Unlock()
	upSet("run", "Memulakan...")
	go doUpdate(force)
	return true
}

// updLoop: semak sekali sehari (3 minit selepas UI hidup, kemudian setiap jam diperiksa sama ada sudah 24 jam).
func updLoop() {
	loadUpdCache()
	time.Sleep(3 * time.Minute)
	for {
		if m := updMode(); m != "off" {
			now := time.Now().Unix()
			last := getUpdCache().At
			if last == 0 || now-last >= 24*3600 || now < last {
				c := checkUpdate()
				if m == "auto" && c.Err == "" && updNewer(c) {
					startUpdate(false)
				}
			}
		}
		time.Sleep(time.Hour)
	}
}

// verifySums menyemak tandatangan senarai semak dalam talian (<tag>-sha256.txt.sig)
// dengan kunci keluaran yang ditanam dalam binari. Binari tanpa kunci keluaran melangkau semakan ini.
func verifySums(sigURL string, sums []byte) error {
	if relpub == "" {
		return nil
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(relpub))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("kunci keluaran dalam binari tidak sah")
	}
	sb, err := fetch(sigURL, 1024, 20*time.Second)
	if err != nil {
		return errors.New("Tandatangan kemas kini tiada: " + shortErr(err))
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sb)))
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, sums, sig) {
		return errors.New("Tandatangan kemas kini tidak sah, dibatalkan")
	}
	return nil
}

// validUpdURL: pelayan kemas kini mesti https dan menunjuk ke fail <tag>-latest.txt.
func validUpdURL(s string) bool {
	if len(s) > 300 || strings.ContainsAny(s, " \t\r\n\"'<>") || !strings.HasPrefix(s, "https://") || !strings.HasSuffix(s, "-latest.txt") {
		return false
	}
	p, err := url.Parse(s)
	return err == nil && p.Host != ""
}
