package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Tukar IP WAN: putus dan sambung semula antara muka wan (netifd, proto mipc).
type wanState struct {
	State     string `json:"state"` // idle | running | done | fail
	Step      string `json:"step"`
	Before    string `json:"before"`
	After     string `json:"after"`
	BeforePub string `json:"beforePub"`
	AfterPub  string `json:"afterPub"`
	Secs      int    `json:"secs"`
	Msg       string `json:"msg"`
}

var (
	wanMu sync.Mutex
	wanSt = wanState{State: "idle"}
)

func runT(d time.Duration, name string, args ...string) error {
	b := findBin(name)
	if b == "" {
		return context.DeadlineExceeded
	}
	ctx, c := context.WithTimeout(context.Background(), d)
	defer c()
	return exec.CommandContext(ctx, b, args...).Run()
}

// directPub: IP awam yang dilihat dari modem (permintaan modem sendiri tidak melalui proksi).
func directPub() string {
	cl := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	for _, u := range []string{"http://api.ipify.org/", "http://ifconfig.me/ip"} {
		r, err := cl.Get(u)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, 128))
		r.Body.Close()
		if ip := strings.TrimSpace(string(b)); net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

func wanSet(f func(*wanState)) {
	wanMu.Lock()
	f(&wanSt)
	wanMu.Unlock()
}

func wanMsg(before, after string) string {
	switch {
	case after == "":
		return "Sambungan pulih tetapi IP awam tidak dapat disemak"
	case before == "":
		return "IP baharu: " + after
	case before != after:
		return "IP awam berubah: " + before + " → " + after
	}
	return "IP awam sama. Operator mungkin memberikan IP yang sama; cuba lagi selepas beberapa minit."
}

func runOut(d time.Duration, name string, args ...string) string {
	b := findBin(name)
	if b == "" {
		return ""
	}
	ctx, c := context.WithTimeout(context.Background(), d)
	defer c()
	out, _ := exec.CommandContext(ctx, b, args...).Output()
	return strings.TrimSpace(string(out))
}

func wanUp() bool {
	d := wanIface()
	return d != "" && ifaceIP(d) != ""
}

// wanWait menunggu WAN pulih sehingga had masa.
func wanWait(max int) bool {
	for i := 0; i < max; i++ {
		if wanUp() {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

// wanCycle: putus sesi data dengan bersih (urutan sama seperti skrip netifd mipc) kemudian sambung.
func wanCycle(apn string) bool {
	_ = runT(40*time.Second, "ifdown", "wan")
	if apn != "" {
		_ = runT(20*time.Second, "mipc_wan_cli", "--data_call_deact_apn", apn)
	}
	time.Sleep(6 * time.Second)
	if runT(40*time.Second, "ifup", "wan") != nil {
		return false
	}
	return wanWait(75)
}

func wanChange() {
	t0 := time.Now()
	secs := func() int { return int(time.Since(t0).Seconds()) }
	wanSet(func(s *wanState) {
		*s = wanState{State: "running", Step: "Menyemak IP semasa", Before: ifaceIP(wanIface()), BeforePub: directPub()}
	})
	apn := runOut(5*time.Second, "uci", "get", "network.wan.apn")
	ok := false
	for try := 1; try <= 2 && !ok; try++ {
		wanSet(func(s *wanState) {
			s.Step = "Menyambung semula data (percubaan " + string(rune('0'+try)) + "/2)"
		})
		ok = wanCycle(apn)
	}
	if !ok {
		wanSet(func(s *wanState) {
			s.State, s.Step, s.Secs = "fail", "", secs()
			s.Msg = "WAN tidak pulih selepas 2 percubaan. Modem dimulakan semula dalam 10 saat."
		})
		time.Sleep(10 * time.Second)
		doReboot()
		return
	}
	pub := ""
	for i := 0; i < 5 && pub == ""; i++ {
		if pub = directPub(); pub == "" {
			time.Sleep(3 * time.Second)
		}
	}
	nd := wanIface()
	wanSet(func(s *wanState) { s.Step = "Memulakan semula VLESS" })
	restartIfActive()
	wanSet(func(s *wanState) {
		s.State, s.Step, s.After, s.AfterPub, s.Secs = "done", "", ifaceIP(nd), pub, secs()
		s.Msg = wanMsg(s.BeforePub, pub)
	})
}

func regWan(mux *http.ServeMux, act func(string, func(*http.Request) (string, bool))) {
	mux.HandleFunc("/api/wanip", func(w http.ResponseWriter, r *http.Request) {
		wanMu.Lock()
		s := wanSt
		wanMu.Unlock()
		jr(w, s)
	})
	act("/api/wanip/run", func(r *http.Request) (string, bool) {
		wanMu.Lock()
		if wanSt.State == "running" {
			wanMu.Unlock()
			return "Sedang berjalan", false
		}
		wanSt.State = "running"
		wanMu.Unlock()
		go wanChange()
		return "Dimulakan", true
	})
}
