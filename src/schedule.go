package main

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"
)

// Jadual kunci band automatik: senarai entri {masa HH:MM, arg band}, disemak setiap minit dan
// ditetapkan sekali sahaja setiap hari (elak ulang panggil set_bandlock setiap minit pada waktu
// sama). Nota reboot berjadual harian sudah wujud berasingan (lihat opts.go "Reboot").

type ScheduleEntry struct {
	Time string `json:"time"` // "HH:MM"
	Arg  string `json:"arg"`  // format sama seperti /api/bandlock/set ("" = buka kunci)
	Note string `json:"note"`
}

var (
	schMu   sync.Mutex
	schList []ScheduleEntry
	schDone = map[string]string{} // "HH:MM" -> tarikh (YYYY-MM-DD) terakhir ditetapkan
)

var schTimeRe = regexp.MustCompile(`^([01]?[0-9]|2[0-3]):([0-5][0-9])$`)

func loadSchedule() []ScheduleEntry {
	var s []ScheduleEntry
	if b, err := os.ReadFile(home + "/etc/bandsched.json"); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveSchedule(s []ScheduleEntry) error {
	b, _ := json.Marshal(s)
	return os.WriteFile(home+"/etc/bandsched.json", b, 0600)
}

func scheduleInit() {
	schMu.Lock()
	schList = loadSchedule()
	schMu.Unlock()
}

func scheduleLoop() {
	scheduleInit()
	for {
		time.Sleep(20 * time.Second)
		now := time.Now()
		hhmm := now.Format("15:04")
		today := now.Format("2006-01-02")
		schMu.Lock()
		for _, e := range schList {
			if e.Time != hhmm {
				continue
			}
			if schDone[e.Time] == today {
				continue
			}
			schDone[e.Time] = today
			arg := e.Arg
			go func(a string) { bandlockApply(a) }(arg)
		}
		schMu.Unlock()
	}
}

func regSchedule(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool)), body func(*http.Request, int64) []byte) {
	mux.HandleFunc("/api/bandsched", func(w http.ResponseWriter, r *http.Request) {
		schMu.Lock()
		list := schList
		schMu.Unlock()
		jr(w, map[string]any{"list": list})
	})
	act("/api/bandsched/set", func(r *http.Request) (string, bool) {
		var in struct {
			List []ScheduleEntry `json:"list"`
		}
		if json.Unmarshal(body(r, 8192), &in) != nil {
			return "Data tidak sah", false
		}
		if len(in.List) > 20 {
			return "Terlalu banyak entri (maksimum 20)", false
		}
		for i, e := range in.List {
			if !schTimeRe.MatchString(e.Time) {
				return "Format masa tidak sah (guna HH:MM)", false
			}
			if e.Arg != "" && !bandlockArgRe.MatchString(e.Arg) {
				return "Format band tidak sah", false
			}
			if len(e.Note) > 64 {
				in.List[i].Note = e.Note[:64]
			}
		}
		if err := saveSchedule(in.List); err != nil {
			return "Gagal simpan: " + err.Error(), false
		}
		schMu.Lock()
		schList = in.List
		schDone = map[string]string{}
		schMu.Unlock()
		return "Jadual disimpan", true
	})
}
