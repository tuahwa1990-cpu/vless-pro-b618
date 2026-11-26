package main

import (
	"encoding/json"
	"net/http"
)

// Paparan isyarat & sel langsung untuk peranti qmodem (guna ubus call qmodem cell_info).
// Respons ubus ialah SATU senarai rata "modem_info" dengan kunci berulang (cth "RSRP" muncul
// dua kali bila ada 2 component carrier/cell aktif). Kami kumpulkan semula kepada kumpulan:
// bila satu kunci muncul BUAT KALI KEDUA dalam kumpulan semasa, itu tanda mula kumpulan (sel) baharu.
// Pendekatan ini berdasarkan struktur sebenar respons peranti (bukan tekaan), dan selamat sebab
// hanya membaca (read-only), tiada arahan ditulis ke modem.

type cellInfoItem struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	FullName string `json:"full_name"`
	Unit     string `json:"unit"`
}

type cellInfoResp struct {
	ModemInfo []cellInfoItem `json:"modem_info"`
}

func qmodemCellGroups(cfg string) ([][]cellInfoItem, error) {
	arg, _ := json.Marshal(map[string]string{"config_section": cfg})
	out, err := qmodemUbusCall("cell_info", string(arg))
	if err != nil {
		return nil, err
	}
	var r cellInfoResp
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return nil, err
	}
	var groups [][]cellInfoItem
	var cur []cellInfoItem
	seen := map[string]bool{}
	for _, it := range r.ModemInfo {
		if seen[it.Key] {
			if len(cur) > 0 {
				groups = append(groups, cur)
			}
			cur = nil
			seen = map[string]bool{}
		}
		seen[it.Key] = true
		cur = append(cur, it)
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups, nil
}

func regSignal(mux *http.ServeMux) {
	mux.HandleFunc("/api/signal", func(w http.ResponseWriter, r *http.Request) {
		cfg, err := qmodemConfigSection()
		if err != nil {
			jr(w, map[string]any{"supported": false})
			return
		}
		groups, err := qmodemCellGroups(cfg)
		if err != nil {
			jr(w, map[string]any{"supported": true, "err": "Gagal baca maklumat isyarat: " + err.Error()})
			return
		}
		jr(w, map[string]any{"supported": true, "groups": groups})
	})
}
