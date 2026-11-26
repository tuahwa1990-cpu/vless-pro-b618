package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Tukar SIM / dual-SIM untuk peranti qmodem yang menyokongnya. Disahkan daripada dispatcher
// sebenar (/usr/share/qmodem/modem_ctrl.sh): case "set_sim_slot") set_sim_slot $3 — iaitu $3
// dihantar TERUS (bukan diekstrak dengan jq macam set_lockband), dan fungsi vendor (quectel.sh)
// guna terus sebagai "AT+QUIMSLOT=$1". Jadi parameter yang dihantar ialah nilai slot itu sendiri
// (cth '"1"'), bukan objek {"sim_slot":"1"}. Kesan jika format meleset: arahan AT gagal/ralat
// sahaja (modem kekal pada slot semasa) — tiada risiko putus/rosak permanent.

type simCaps struct {
	SupportSwitch string   `json:"supportSwitch"`
	SimSlots      []string `json:"simSlots"`
}
type simSlotResp struct {
	SimSlot string `json:"sim_slot"`
}

func qmodemSimCaps(cfg string) (*simCaps, error) {
	arg, _ := json.Marshal(map[string]string{"config_section": cfg})
	out, err := qmodemUbusCall("get_sim_switch_capabilities", string(arg))
	if err != nil {
		return nil, err
	}
	var c simCaps
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func qmodemSimSlot(cfg string) (string, error) {
	arg, _ := json.Marshal(map[string]string{"config_section": cfg})
	out, err := qmodemUbusCall("get_sim_slot", string(arg))
	if err != nil {
		return "", err
	}
	var r simSlotResp
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return "", err
	}
	return r.SimSlot, nil
}

func qmodemSetSimSlot(slot string) (string, error) {
	arg, _ := json.Marshal(slot) // bare JSON string, cth "1" -> `"1"`
	return qmodemUbusCall("set_sim_slot", string(arg))
}

func regSim(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/sim", func(w http.ResponseWriter, r *http.Request) {
		cfg, err := qmodemConfigSection()
		if err != nil {
			jr(w, map[string]any{"supported": false})
			return
		}
		caps, err := qmodemSimCaps(cfg)
		if err != nil || caps.SupportSwitch != "1" || len(caps.SimSlots) < 2 {
			jr(w, map[string]any{"supported": false})
			return
		}
		cur, err := qmodemSimSlot(cfg)
		if err != nil {
			jr(w, map[string]any{"supported": true, "err": "Gagal baca slot SIM semasa: " + err.Error()})
			return
		}
		jr(w, map[string]any{"supported": true, "slots": caps.SimSlots, "current": cur})
	})
	act("/api/sim/set", func(r *http.Request) (string, bool) {
		slot := strings.TrimSpace(string(body(r, 16)))
		if slot == "" || len(slot) > 4 {
			return "Slot tidak sah", false
		}
		if _, err := qmodemConfigSection(); err != nil {
			return "Ciri ini belum disokong pada modem ini", false
		}
		out, err := qmodemSetSimSlot(slot)
		if err != nil {
			return "Gagal tukar SIM: " + err.Error(), false
		}
		return "Arahan tukar SIM dihantar (" + out + "). Semak semula sambungan selepas beberapa saat.", true
	})
}
