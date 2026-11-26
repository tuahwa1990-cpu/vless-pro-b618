package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// Notifikasi Telegram untuk peristiwa penting (kuota, WAN putus/pulih). Pengguna masukkan token
// bot dan chat ID sendiri (dicipta melalui @BotFather); kami hanya hantar mesej teks ringkas.

type NotifyCfg struct {
	On        bool   `json:"on"`
	BotToken  string `json:"bot_token"`
	ChatID    string `json:"chat_id"`
	QuotaWarn bool   `json:"quota_warn"`
	QuotaFull bool   `json:"quota_full"`
	WanDown   bool   `json:"wan_down"`
	WanUp     bool   `json:"wan_up"`
}

var (
	nMu  sync.Mutex
	nCfg NotifyCfg
)

func loadNotify() NotifyCfg {
	var c NotifyCfg
	if b, err := os.ReadFile(home + "/etc/notify.json"); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveNotifyCfg(c NotifyCfg) error {
	b, _ := json.Marshal(c)
	return os.WriteFile(home+"/etc/notify.json", b, 0600)
}

func notifyInit() {
	nMu.Lock()
	nCfg = loadNotify()
	nMu.Unlock()
}

// notifyRawSend hantar mesej jika ciri dihidupkan dan token/chat id ada. Tidak menyekat (blocking)
// panggil pemanggil — dihantar dalam goroutine sendiri, dengan had masa supaya tak tersekat lama
// jika rangkaian Telegram tidak dapat dicapai.
func notifyRawSend(msg string) {
	nMu.Lock()
	c := nCfg
	nMu.Unlock()
	if !c.On || c.BotToken == "" || c.ChatID == "" {
		return
	}
	go func() {
		form := url.Values{}
		form.Set("chat_id", c.ChatID)
		form.Set("text", msg)
		req, err := http.NewRequest("POST", "https://api.telegram.org/bot"+c.BotToken+"/sendMessage", bytes.NewBufferString(form.Encode()))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		cl := &http.Client{Timeout: 10 * time.Second}
		if resp, err := cl.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
}

func notifyQuotaWarn(pct float64) {
	nMu.Lock()
	on := nCfg.On && nCfg.QuotaWarn
	nMu.Unlock()
	if on {
		notifyRawSend(fmt.Sprintf("⚠️ VLESS Pro: kuota data hampir habis (%.0f%% digunakan).", pct*100))
	}
}

func notifyQuotaFull() {
	nMu.Lock()
	on := nCfg.On && nCfg.QuotaFull
	nMu.Unlock()
	if on {
		notifyRawSend("🚫 VLESS Pro: kuota data habis, internet disekat.")
	}
}

func notifySendWanDown() {
	nMu.Lock()
	on := nCfg.On && nCfg.WanDown
	nMu.Unlock()
	if on {
		notifyRawSend("🔴 VLESS Pro: sambungan WAN modem terputus.")
	}
}

func notifySendWanUp() {
	nMu.Lock()
	on := nCfg.On && nCfg.WanUp
	nMu.Unlock()
	if on {
		notifyRawSend("🟢 VLESS Pro: sambungan WAN modem pulih.")
	}
}

func regNotify(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/notify", func(w http.ResponseWriter, r *http.Request) {
		nMu.Lock()
		c := nCfg
		nMu.Unlock()
		jr(w, map[string]any{"cfg": c})
	})
	act("/api/notify/set", func(r *http.Request) (string, bool) {
		var c NotifyCfg
		if json.Unmarshal(body(r, 4096), &c) != nil {
			return "Data tidak sah", false
		}
		if len(c.BotToken) > 200 || len(c.ChatID) > 64 {
			return "Token atau chat ID terlalu panjang", false
		}
		if err := saveNotifyCfg(c); err != nil {
			return "Gagal simpan: " + err.Error(), false
		}
		nMu.Lock()
		nCfg = c
		nMu.Unlock()
		return "Tetapan notifikasi disimpan", true
	})
	act("/api/notify/test", func(r *http.Request) (string, bool) {
		nMu.Lock()
		c := nCfg
		nMu.Unlock()
		if c.BotToken == "" || c.ChatID == "" {
			return "Isi token bot dan chat ID dahulu", false
		}
		notifyRawSend("✅ VLESS Pro: ini mesej ujian. Jika anda terima ini, notifikasi berfungsi.")
		return "Mesej ujian dihantar", true
	})
}
