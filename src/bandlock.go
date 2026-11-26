package main

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// Kunci band (5G NR / 4G LTE / 3G UMTS) untuk modem yang menyokongnya (AW1000 / AW1000 Ultra —
// dikesan melalui kewujudan /bin/modem_status). Mekanisme ni ikut cara firmware vendor sendiri
// buat (lihat /www/luci-static/resources/view/modem/lockband.js pada peranti):
//   - Baca:  modem_status --get_bandlock   -> JSON dengan current_nr_band/support_nr_band,
//            current_lte_band/support_lte_band, current_umts_band/support_umts_band
//            (setiap nilai rentetan dipisah '/', cth "n1/n3/n41" atau "B1/B3/B7").
//   - Tulis: modem_status --set_bandlock "<rat>,<band>/<band>;<rat>,<band>" (cth "5,1/3/41;4,3/7"),
//            disimpan ke uci 3ginfo.@3ginfo[0].bandlock, kemudian radio dimulakan semula
//            (AT+CFUN=4 -> tunggu -> AT+CFUN=1 -> tunggu -> ifup wan/wan6).
// B618 (Huawei) tiada /bin/modem_status, jadi ciri ni dilaporkan "unsupported" di situ.

const modemStatusBin = "/bin/modem_status"

func bandlockSupported() bool {
	st, err := os.Stat(modemStatusBin)
	return err == nil && !st.IsDir()
}

// bandlockArgRe mengesahkan format "<rat>,<band>[/<band>...][;<rat>,<band>[/<band>...]]*"
// sebelum dihantar ke modem_status (elak hantar data sampah ke modem).
var bandlockArgRe = regexp.MustCompile(`^[0-9]+,[0-9]+(/[0-9]+)*(;[0-9]+,[0-9]+(/[0-9]+)*)*$`)

func bandlockGet() (string, error) {
	out, err := run2(modemStatusBin, "--get_bandlock")
	if err != nil {
		return "", err
	}
	return out, nil
}

func bandlockStored() string {
	v, _ := run2("uci", "-q", "get", "3ginfo.@3ginfo[0].bandlock")
	return strings.TrimSpace(v)
}

func bandlockSave(arg string) error {
	if arg == "" {
		_, _ = run2("uci", "-q", "delete", "3ginfo.@3ginfo[0].bandlock")
	} else {
		if _, err := run2("uci", "set", "3ginfo.@3ginfo[0].bandlock="+arg); err != nil {
			return err
		}
	}
	_, err := run2("uci", "commit", "3ginfo")
	return err
}

// restartRadioAW ikut urutan sama seperti UI vendor: CFUN=4 (radio off) -> tunggu -> CFUN=1
// (radio on) -> tunggu -> ifup wan/wan6, supaya tetapan band baharu berkuat kuasa.
func restartRadioAW() {
	_, _ = run2(modemStatusBin, "--at_cmd", "AT+CFUN=4")
	time.Sleep(2 * time.Second)
	_, _ = run2(modemStatusBin, "--at_cmd", "AT+CFUN=1")
	time.Sleep(4 * time.Second)
	_, _ = run2("ifup", "wan")
	_, _ = run2("ifup", "wan6")
}

func regBandlock(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/bandlock", func(w http.ResponseWriter, r *http.Request) {
		if bandlockSupported() {
			raw, err := bandlockGet()
			if err != nil {
				jr(w, map[string]any{"supported": true, "err": "Gagal baca status band daripada modem: " + err.Error()})
				return
			}
			jr(w, map[string]any{"supported": true, "raw": raw, "stored": bandlockStored()})
			return
		}
		if cfg, err := qmodemConfigSection(); err == nil {
			qr, err := qmodemGetLockband(cfg)
			if err != nil {
				jr(w, map[string]any{"supported": true, "err": "Gagal baca status band daripada modem: " + err.Error()})
				return
			}
			jr(w, map[string]any{"supported": true, "raw": qmodemBuildRaw(qr), "stored": ""})
			return
		}
		jr(w, map[string]any{"supported": false})
	})
	act("/api/bandlock/set", func(r *http.Request) (string, bool) {
		var in struct {
			Arg string `json:"arg"`
		}
		if json.Unmarshal(body(r, 2048), &in) != nil {
			return "Data tidak sah", false
		}
		arg := strings.TrimSpace(in.Arg)
		if arg != "" && !bandlockArgRe.MatchString(arg) {
			return "Format band tidak sah", false
		}
		if bandlockSupported() {
			if _, err := run2(modemStatusBin, "--set_bandlock", arg); err != nil {
				return "Gagal tetapkan kunci band: " + err.Error(), false
			}
			if err := bandlockSave(arg); err != nil {
				return "Kunci band ditetapkan tetapi gagal simpan: " + err.Error(), false
			}
			go restartRadioAW()
			if arg == "" {
				return "Kunci band dibuka (automatik semula). Radio sedang dimulakan semula...", true
			}
			return "Kunci band ditetapkan. Radio sedang dimulakan semula (ambil masa ~6 saat)...", true
		}
		if cfg, err := qmodemConfigSection(); err == nil {
			if err := qmodemApplyArg(cfg, arg); err != nil {
				return "Gagal tetapkan kunci band: " + err.Error(), false
			}
			if arg == "" {
				return "Kunci band dibuka (automatik semula).", true
			}
			return "Kunci band ditetapkan.", true
		}
		return "Ciri kunci band belum disokong pada modem ini", false
	})
}
