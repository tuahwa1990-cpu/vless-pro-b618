package main

import (
	"encoding/json"
	"net/http"
	"os"
)

// Sekat iklan: peraturan penghalaan xray (geosite:category-ads-all -> block) pada trafik TCP
// (domain diambil daripada SNI/Host melalui sniffing). Memerlukan geosite.dat.
type AdCfg struct {
	On bool `json:"on"`
}

func loadAd() AdCfg {
	var c AdCfg
	if b, err := os.ReadFile(home + "/etc/adblock.json"); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// adActive: peraturan hanya dimasukkan jika dihidupkan DAN data geosite wujud.
func adActive() bool {
	if !loadAd().On {
		return false
	}
	_, ok := geoFile("geosite")
	return ok
}

func adRule() map[string]any {
	return map[string]any{"type": "field", "domain": []any{"geosite:category-ads-all"}, "outboundTag": "block"}
}

func regAd(mux *http.ServeMux, body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/adblock", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if locked(readLic()) {
				jr(w, map[string]any{"ok": false, "msg": "Lesen belum aktif"})
				return
			}
			var c AdCfg
			if json.Unmarshal(body(r, 256), &c) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Data tidak sah"})
				return
			}
			b, _ := json.Marshal(c)
			if os.WriteFile(home+"/etc/adblock.json", b, 0600) != nil {
				jr(w, map[string]any{"ok": false, "msg": "Gagal simpan"})
				return
			}
			_, have := geoFile("geosite")
			msg := "Sekat iklan dimatikan"
			if c.On {
				msg = "Sekat iklan dihidupkan"
				if !have {
					geoSt.Lock()
					start := !geoSt.run
					if start {
						geoSt.run, geoSt.msg = true, "Bermula..."
					}
					geoSt.Unlock()
					if start {
						go geoDownload() // memulakan semula xray bila siap
					}
					msg = "Memuat turun senarai iklan..."
				}
			}
			if have || !c.On {
				restartIfActive()
			}
			jr(w, map[string]any{"ok": true, "msg": msg})
			return
		}
		sz := int64(0)
		if p, ok := geoFile("geosite"); ok {
			if st, err := os.Stat(p); err == nil {
				sz = st.Size()
			}
		}
		geoSt.Lock()
		run, msg := geoSt.run, geoSt.msg
		geoSt.Unlock()
		jr(w, map[string]any{"cfg": loadAd(), "geosite": sz, "active": adActive(), "run": run, "msg": msg})
	})
}
