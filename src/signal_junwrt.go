package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Isyarat & Sel untuk AW1000 Ultra (JunWRT, modem MTK Fibocom FG360, protokol MIPC).
//
// Sejarah (disahkan langsung pada peranti, bukan agakan):
//  1. Mulanya guna /usr/bin/junwrt-signal — daemon vendor yang simpan sejarah dalam ingatan, tapi
//     disahkan (baca kod skrip) ia cuma ambil SATU sampel setiap 120 saat (INTERVAL=120, hardcode).
//     Itu punca paparan nampak "beku".
//  2. Tukar kepada panggil terus /usr/share/3ginfo-lite/3ginfo.sh json (skrip laman "Overview" asal)
//     setiap kali — disahkan live (nilai berubah antara panggilan), tapi ambil ~1.5-2 saat setiap
//     panggilan sebab ia buat BANYAK kerja lain yang tak perlu untuk isyarat sahaja: jalankan skrip
//     detect.sh, rangkaikan 7+ arahan AT (mipc_wan_cli --at_cmd "AT+CPIN?;+CSQ;+COPS...), AT+CGMI,
//     AT+CGMM, AT+EDMFAPP (kadar MAC), kumpul IMEI/ICCID/IMSI/suhu/jumlah data, dll — semuanya cuma
//     untuk akhirnya baca /tmp/modem_cell_info.json & /tmp/modem_signal_strength.json yang ditulis
//     oleh binari vendor /bin/modem_status.
//  3. Disahkan (baca kod sumber 3ginfo.sh dan modem/mtk/fibocom/fg360): binari /bin/modem_status
//     sendiri yang tulis fail-fail JSON tu (--cell_info, --ca_info, --band_info, --sign_info), dan ia
//     SANGAT laju — diuji langsung: 4 panggilan sekali gus siap DALAM SAAT YANG SAMA (<1s jumlah),
//     berbanding ~1.5-2s untuk SATU panggilan 3ginfo.sh penuh. Jadi kami panggil binari vendor ini
//     terus dan baca fail JSON-nya, skip semua kerja 3ginfo.sh yang tak berkaitan isyarat.
func junwrtSignalSupported() bool {
	st, err := os.Stat(modemStatusBin)
	return err == nil && !st.IsDir()
}

func modemStatusRun(flag string) {
	_, _ = run2(modemStatusBin, flag)
}

func readJSONFile(path string, v interface{}) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

type ginCellEntry struct {
	CID  json.Number `json:"cid"`
	RSSI json.Number `json:"rssi"`
	RSRP json.Number `json:"rsrp"`
	RSRQ json.Number `json:"rsrq"`
	SINR json.Number `json:"sinr"`
}

type ginCellInfo struct {
	ServingRat string         `json:"serving_rat"`
	SaInfo     []ginCellEntry `json:"sa_info"`
	NsaLteInfo []ginCellEntry `json:"nsa_lte_info"`
	LteInfo    []ginCellEntry `json:"lte_info"`
	UmtsInfo   []ginCellEntry `json:"umts_info"`
	GsmInfo    []ginCellEntry `json:"gsm_info"`
}

type ginBandInfo struct {
	LteBand json.Number `json:"lte_band"`
	NrBand  json.Number `json:"nr_band"`
}

type ginCaInfo struct {
	NrSccNum  json.Number `json:"nr_scc_num"`
	NrSccInfo []struct {
		NrSccBand json.Number `json:"nr_scc_band"`
	} `json:"nr_scc_info"`
	LteSccNum json.Number `json:"lte_scc_num"`
}

type ginCCEntry struct {
	Type       string      `json:"nr5g_cc_type"`
	RssiRx0    json.Number `json:"rssi_rx0"`
	TrsRsrpRx0 json.Number `json:"trs_rsrp_rx0"`
	TrsSnrRx0  json.Number `json:"trs_snr_rx0"`
	SsbRsrpRx0 json.Number `json:"ssb_rsrp_rx0"`
	SsbSnrRx0  json.Number `json:"ssb_snr_rx0"`
}

type ginSignalStrength struct {
	NrInfo []struct {
		CCInfo []ginCCEntry `json:"nr_cc_info"`
	} `json:"nr_info"`
}

func numStr(n json.Number) string {
	s := n.String()
	if s == "" {
		return ""
	}
	return s
}

// ccValue mengikut keutamaan fg360: trs_* dulu (jika bukan "0"/"-255"/kosong), kalau tidak ssb_*.
func ccValue(trs, ssb json.Number) string {
	t := numStr(trs)
	if t != "" && t != "0" && t != "-255" {
		return t
	}
	s := numStr(ssb)
	if s != "" && s != "-255" {
		return s
	}
	return ""
}

// junwrtSignalGroups memulangkan bacaan isyarat terkini (baca terus /bin/modem_status, bukan cache)
// dalam bentuk [][]cellInfoItem yang SAMA seperti laluan qmodem, supaya ui.html/apl Android tidak
// perlu diubah. Sel pertama ialah band utama (serving cell), sel kedua (jika ada carrier agregasi 5G
// aktif) ialah band sekunder (SCC).
func junwrtSignalGroups() ([][]cellInfoItem, error) {
	modemStatusRun("--cell_info")
	modemStatusRun("--band_info")
	modemStatusRun("--ca_info")
	modemStatusRun("--sign_info")

	var cell ginCellInfo
	if err := readJSONFile("/tmp/modem_cell_info.json", &cell); err != nil {
		return nil, fmt.Errorf("gagal baca maklumat sel: %w", err)
	}
	var band ginBandInfo
	_ = readJSONFile("/tmp/modem_band_info.json", &band)

	var entries []ginCellEntry
	bandLabel := ""
	switch cell.ServingRat {
	case "NR-SA":
		entries = cell.SaInfo
		bandLabel = "n" + numStr(band.NrBand)
	case "NR-NSA":
		entries = cell.NsaLteInfo
		bandLabel = "B" + numStr(band.LteBand)
	case "LTE":
		entries = cell.LteInfo
		bandLabel = "B" + numStr(band.LteBand)
	case "UMTS":
		entries = cell.UmtsInfo
	case "GSM":
		entries = cell.GsmInfo
	}
	if len(entries) == 0 {
		return [][]cellInfoItem{}, nil
	}
	e := entries[0]
	if bandLabel == "n" || bandLabel == "B" {
		bandLabel = "" // nombor band tiada (0 atau kosong)
	}
	group1 := []cellInfoItem{
		{Key: "RSRP", Value: numStr(e.RSRP), FullName: "RSRP", Unit: "dBm"},
		{Key: "SINR", Value: numStr(e.SINR), FullName: "SINR", Unit: "dB"},
		{Key: "RSRQ", Value: numStr(e.RSRQ), FullName: "RSRQ", Unit: "dB"},
		{Key: "RSSI", Value: numStr(e.RSSI), FullName: "RSSI", Unit: "dBm"},
		{Key: "BAND", Value: bandLabel, FullName: "Jalur/Rangkaian"},
	}
	groups := [][]cellInfoItem{group1}

	// Carrier sekunder (5G CA) — hanya untuk NR-SA/NR-NSA, bila ada SCC aktif.
	if cell.ServingRat == "NR-SA" || cell.ServingRat == "NR-NSA" {
		var ca ginCaInfo
		if err := readJSONFile("/tmp/modem_ca_info.json", &ca); err == nil && numStr(ca.NrSccNum) != "" && numStr(ca.NrSccNum) != "0" && len(ca.NrSccInfo) > 0 {
			sccBand := "n" + numStr(ca.NrSccInfo[0].NrSccBand)
			var sig ginSignalStrength
			var scc *ginCCEntry
			if err := readJSONFile("/tmp/modem_signal_strength.json", &sig); err == nil {
				for _, ni := range sig.NrInfo {
					for i := range ni.CCInfo {
						if ni.CCInfo[i].Type != "NR PCC" {
							scc = &ni.CCInfo[i]
						}
					}
				}
			}
			if scc != nil {
				group2 := []cellInfoItem{
					{Key: "RSRP", Value: ccValue(scc.TrsRsrpRx0, scc.SsbRsrpRx0), FullName: "RSRP", Unit: "dBm"},
					{Key: "SINR", Value: ccValue(scc.TrsSnrRx0, scc.SsbSnrRx0), FullName: "SINR", Unit: "dB"},
					{Key: "RSSI", Value: numStr(scc.RssiRx0), FullName: "RSSI", Unit: "dBm"},
					{Key: "BAND", Value: sccBand, FullName: "Jalur/Rangkaian"},
				}
				groups = append(groups, group2)
			}
		}
	}

	return groups, nil
}
