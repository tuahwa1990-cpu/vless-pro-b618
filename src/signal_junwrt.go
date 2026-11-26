package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Isyarat & Sel untuk AW1000 Ultra (JunWRT).
//
// Mulanya kami guna /usr/bin/junwrt-signal (daemon vendor yang simpan sejarah dalam ingatan),
// tetapi disahkan langsung pada peranti (baca kod skrip itu sendiri) ia hanya ambil SATU sampel
// setiap 120 saat (INTERVAL=120, hardcode, tiada cara ubah). Itu punca paparan nampak "beku" bila
// pengguna gerakkan modem untuk cari isyarat terbaik.
//
// junwrt-signal sendiri dapat data dengan memanggil /usr/share/3ginfo-lite/3ginfo.sh json — skrip
// LIVE yang sama digunakan laman "Overview" asal firmware. Disahkan live (bukan cache): 2 panggilan
// berjarak 3 saat memberi nilai berbeza (mac_rate, prssi, s1rsrp, s1rsrq semua berubah), ambil masa
// ~1.5 saat setiap panggilan. Jadi kami panggil skrip ini TERUS setiap kali /api/signal diminta,
// supaya bacaan sentiasa segar dan sesuai untuk pantau masa nyata semasa gerakkan modem.
const ginfoLiteScript = "/usr/share/3ginfo-lite/3ginfo.sh"

func junwrtSignalSupported() bool {
	st, err := os.Stat(ginfoLiteScript)
	return err == nil && !st.IsDir()
}

func ginfoLiteJSON() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ginfoLiteScript, "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gagal jalankan 3ginfo.sh: %w", err)
	}
	s := string(out)
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return nil, fmt.Errorf("respons tidak dijangka daripada 3ginfo.sh")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s[i:j+1]), &m); err != nil {
		return nil, fmt.Errorf("gagal baca respons 3ginfo.sh: %w", err)
	}
	return m, nil
}

// ginfoNum ambil nombor pertama sahaja daripada nilai macam "-104", "-75 dBm" atau
// "-75 dBm | -74 dBm | -75 dBm | -78 dBm" (anggap "-" atau kosong sebagai tiada data).
func ginfoNum(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "-" {
		return ""
	}
	if sp := strings.IndexAny(v, " |"); sp >= 0 {
		v = strings.TrimSpace(v[:sp])
	}
	return v
}

// ginfoBand ambil label jalur sahaja, cth "n78 (3500 MHz)" -> "n78".
func ginfoBand(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "-" {
		return ""
	}
	if sp := strings.IndexByte(v, ' '); sp >= 0 {
		v = v[:sp]
	}
	return v
}

// junwrtSignalGroups memulangkan bacaan isyarat terkini dalam bentuk [][]cellInfoItem yang SAMA
// seperti laluan qmodem, supaya ui.html (rSignal/rBandlock) dan apl Android tidak perlu diubah.
// Sel pertama ialah band utama (Primary Component Carrier), sel kedua (jika ada carrier agregasi
// aktif) ialah band sekunder.
func junwrtSignalGroups() ([][]cellInfoItem, error) {
	m, err := ginfoLiteJSON()
	if err != nil {
		return nil, err
	}
	group := func(rsrp, sinr, rsrq, rssi, band string) []cellInfoItem {
		return []cellInfoItem{
			{Key: "RSRP", Value: ginfoNum(m[rsrp]), FullName: "RSRP", Unit: "dBm"},
			{Key: "SINR", Value: ginfoNum(m[sinr]), FullName: "SINR", Unit: "dB"},
			{Key: "RSRQ", Value: ginfoNum(m[rsrq]), FullName: "RSRQ", Unit: "dB"},
			{Key: "RSSI", Value: ginfoNum(m[rssi]), FullName: "RSSI", Unit: "dBm"},
			{Key: "BAND", Value: ginfoBand(m[band]), FullName: "Jalur/Rangkaian"},
		}
	}
	groups := [][]cellInfoItem{group("rsrp", "sinr", "rsrq", "rssi", "pband")}
	if ginfoBand(m["s1band"]) != "" {
		groups = append(groups, group("s1rsrp", "s1sinr", "s1rsrq", "s1rssi", "s1band"))
	}
	return groups, nil
}
