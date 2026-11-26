package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Isyarat & Sel untuk AW1000 Ultra (JunWRT): guna daemon vendor sedia ada /usr/bin/junwrt-signal
// yang mengambil sampel isyarat secara berkala dan menyimpannya dalam ingatan (bukan kami yang
// polling AT command sendiri). Disahkan langsung pada peranti:
//   junwrt-signal status            -> {"enabled":0|1,"running":1,"interval":120,"now":<unix>,
//                                        "samples":[[ts,rsrp,sinr,rsrq,band], ...]}
//   junwrt-signal save enabled=0|1  -> hidup/matikan pengambilan sampel.
// "enabled" perlu 1 sebelum "samples" diisi (satu sampel setiap ~"interval" saat). Kami aktifkan
// secara automatik pada capaian pertama supaya pengguna tak perlu buat apa-apa.

const junwrtSignalBin = "/usr/bin/junwrt-signal"

func junwrtSignalSupported() bool {
	st, err := os.Stat(junwrtSignalBin)
	return err == nil && !st.IsDir()
}

type junwrtSignalStatus struct {
	Enabled  int             `json:"enabled"`
	Running  int             `json:"running"`
	Interval int             `json:"interval"`
	Now      int64           `json:"now"`
	Samples  [][]interface{} `json:"samples"`
}

func junwrtSignalRead() (*junwrtSignalStatus, error) {
	out, err := run2(junwrtSignalBin, "status")
	if err != nil {
		return nil, err
	}
	var s junwrtSignalStatus
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// junwrtSignalGroups memulangkan bacaan isyarat terkini dalam bentuk [][]cellInfoItem yang SAMA
// seperti laluan qmodem, supaya ui.html (rSignal) tidak perlu diubah langsung.
func junwrtSignalGroups() ([][]cellInfoItem, error) {
	s, err := junwrtSignalRead()
	if err != nil {
		return nil, err
	}
	if s.Enabled == 0 {
		// Aktifkan pengambilan sampel secara automatik; sampel pertama akan ada lepas ~interval saat.
		_, _ = run2(junwrtSignalBin, "save", "enabled=1")
	}
	if len(s.Samples) == 0 {
		return [][]cellInfoItem{}, nil
	}
	last := s.Samples[len(s.Samples)-1]
	get := func(i int) string {
		if i >= len(last) || last[i] == nil {
			return ""
		}
		switch v := last[i].(type) {
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case string:
			return v
		default:
			return fmt.Sprint(v)
		}
	}
	group := []cellInfoItem{
		{Key: "RSRP", Value: get(1), FullName: "RSRP", Unit: "dBm"},
		{Key: "SINR", Value: get(2), FullName: "SINR", Unit: "dB"},
		{Key: "RSRQ", Value: get(3), FullName: "RSRQ", Unit: "dB"},
		{Key: "BAND", Value: get(4), FullName: "Jalur/Rangkaian"},
	}
	return [][]cellInfoItem{group}, nil
}
