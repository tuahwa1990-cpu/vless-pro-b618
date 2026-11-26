package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Kunci band untuk peranti yang guna pakej "qmodem"/"tom_modem" (cth AW1000 biasa dengan
// modem USB Quectel sebenar seperti RG500Q), berbeza daripada mekanisme /bin/modem_status
// AW1000 Ultra. Disahkan terus daripada kod sumber peranti sendiri:
//   /usr/share/qmodem/modem_ctrl.sh   (dispatch ubus "set_lockband" -> fungsi set_lockband)
//   /usr/share/qmodem/vendor/quectel.sh (set_lockband -> set_lockband_nr untuk cip 5G)
//
// Baca:  ubus call qmodem get_lockband '{"config_section":"<seksyen>"}'
//        -> {"lockband":{"UMTS":{"available_band":[...],"lock_band":[...]},
//                        "LTE":{...},"NR_SA":{...},"NR_NSA":{...}}}
// Tulis: ubus call qmodem set_lockband
//        '{"config_section":"<seksyen>","params":"{\"band_class\":\"<kelas>\",\"lock_band\":\"<id,id>\"}"}'
//        dengan <kelas> salah satu: "UMTS","LTE","NR_NSA","NR" (NR = NR_SA bila baca).
// Nota: QNWPREFCFG berkuat kuasa tanpa perlu kitar semula CFUN (tiada langkah begitu
// dalam skrip asal), jadi kami tidak paksa radio restart di sini.

const qmodemUbusObj = "qmodem"

type qmBandEntry struct {
	BandID string `json:"band_id"`
}
type qmGroup struct {
	AvailableBand []qmBandEntry `json:"available_band"`
	LockBand      []string      `json:"lock_band"`
}
type qmGetResp struct {
	Lockband map[string]qmGroup `json:"lockband"`
}

// pemetaan kunci luaran (sama seperti dipakai UI/app sedia ada) <-> kunci get_lockband <-> nilai
// band_class untuk set_lockband.
var qmRatMap = []struct {
	ext      string // "nr" | "lte" | "umts" (padan dengan rat "5"/"4"/"3" pada UI)
	getKey   string
	setClass string
}{
	{"nr", "NR_NSA", "NR_NSA"},
	{"lte", "LTE", "LTE"},
	{"umts", "UMTS", "UMTS"},
}

func qmodemSupported() bool {
	out, err := run2("ubus", "list")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == qmodemUbusObj {
			return true
		}
	}
	return false
}

// qmodemConfigSection cari nama seksyen UCI jenis "modem-device" di bawah "qmodem"
// (cth "4_1"), iaitu nilai "config_section" yang perlu dihantar pada setiap panggilan ubus.
func qmodemConfigSection() (string, error) {
	out, err := run2("uci", "show", "qmodem")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasSuffix(line, "=modem-device") {
			continue
		}
		// format: qmodem.<seksyen>=modem-device
		name := strings.TrimPrefix(line, "qmodem.")
		name = strings.TrimSuffix(name, "=modem-device")
		if name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("tiada peranti modem dijumpai dalam konfigurasi qmodem")
}

func qmodemUbusCall(method, argJSON string) (string, error) {
	return run2("ubus", "call", qmodemUbusObj, method, argJSON)
}

func qmodemGetLockband(cfg string) (*qmGetResp, error) {
	arg, _ := json.Marshal(map[string]string{"config_section": cfg})
	out, err := qmodemUbusCall("get_lockband", string(arg))
	if err != nil {
		return nil, err
	}
	var r qmGetResp
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return nil, fmt.Errorf("respons tidak sah daripada modem: %w", err)
	}
	return &r, nil
}

// qmodemBuildRaw menukar respons get_lockband kepada format JSON "raw" yang sama seperti
// /bin/modem_status --get_bandlock (current_nr_band/support_nr_band dll, nilai dipisah '/'),
// supaya UI web dan apl sedia ada (SubBandlock / rBandlock) berfungsi tanpa diubah.
func qmodemBuildRaw(r *qmGetResp) string {
	out := map[string]string{}
	for _, m := range qmRatMap {
		g, ok := r.Lockband[m.getKey]
		if !ok {
			continue
		}
		var avail []string
		for _, b := range g.AvailableBand {
			if b.BandID != "" && b.BandID != "0" {
				avail = append(avail, b.BandID)
			}
		}
		var lock []string
		for _, b := range g.LockBand {
			if b != "" && b != "0" {
				lock = append(lock, b)
			}
		}
		out["support_"+m.ext+"_band"] = strings.Join(avail, "/")
		out["current_"+m.ext+"_band"] = strings.Join(lock, "/")
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// qmodemAvailableBands bagi satu RAT ("nr"/"lte"/"umts"), untuk tujuan "buka kunci"
// (tetapkan semula kepada semua band yang disokong = tiada sekatan).
func qmodemAvailableBands(r *qmGetResp, ext string) []string {
	for _, m := range qmRatMap {
		if m.ext != ext {
			continue
		}
		g, ok := r.Lockband[m.getKey]
		if !ok {
			return nil
		}
		var avail []string
		for _, b := range g.AvailableBand {
			if b.BandID != "" && b.BandID != "0" {
				avail = append(avail, b.BandID)
			}
		}
		return avail
	}
	return nil
}

func qmodemSetLockband(cfg, ext string, bandIDs []string) error {
	var setClass string
	for _, m := range qmRatMap {
		if m.ext == ext {
			setClass = m.setClass
		}
	}
	if setClass == "" {
		return fmt.Errorf("jenis rangkaian tidak disokong: %s", ext)
	}
	inner, _ := json.Marshal(map[string]string{
		"band_class": setClass,
		"lock_band":  strings.Join(bandIDs, ","),
	})
	outer, _ := json.Marshal(map[string]string{
		"config_section": cfg,
		"params":         string(inner),
	})
	_, err := qmodemUbusCall("set_lockband", string(outer))
	return err
}

// qmodemApplyArg terima format arg yang sama seperti UI sedia ada: "<rat>,<band>/<band>;..."
// dengan rat "5"=nr, "4"=lte, "3"=umts. Bagi setiap RAT yang TIDAK disebut dalam arg (bila arg
// kosong = buka kunci semua), band ditetapkan semula kepada senarai penuh yang disokong.
func qmodemApplyArg(cfg, arg string) error {
	r, err := qmodemGetLockband(cfg)
	if err != nil {
		return fmt.Errorf("gagal baca status band semasa: %w", err)
	}
	want := map[string][]string{}
	if arg != "" {
		for _, grp := range strings.Split(arg, ";") {
			parts := strings.SplitN(grp, ",", 2)
			if len(parts) != 2 {
				continue
			}
			var ext string
			switch parts[0] {
			case "5":
				ext = "nr"
			case "4":
				ext = "lte"
			case "3":
				ext = "umts"
			default:
				continue
			}
			var bands []string
			for _, b := range strings.Split(parts[1], "/") {
				if b != "" {
					bands = append(bands, b)
				}
			}
			if len(bands) > 0 {
				want[ext] = bands
			}
		}
	}
	for _, m := range qmRatMap {
		bands, ok := want[m.ext]
		if !ok {
			bands = qmodemAvailableBands(r, m.ext)
			if len(bands) == 0 {
				continue // RAT ini tiada pada modem, langkau
			}
		}
		if err := qmodemSetLockband(cfg, m.ext, bands); err != nil {
			return fmt.Errorf("gagal tetapkan band %s: %w", m.ext, err)
		}
	}
	return nil
}
