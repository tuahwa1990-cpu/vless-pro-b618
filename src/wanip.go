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

func wanChange() {
	t0 := time.Now()
	dev := wanIface()
	wanSet(func(s *wanState) {
		*s = wanState{State: "running", Step: "Menyemak IP semasa", Before: ifaceIP(dev), BeforePub: directPub()}
	})
	wanSet(func(s *wanState) { s.Step = "Memutuskan sambungan data" })
	_ = runT(40*time.Second, "ifdown", "wan")
	time.Sleep(3 * time.Second)
	wanSet(func(s *wanState) { s.Step = "Menyambung semula" })
	if runT(40*time.Second, "ifup", "wan") != nil {
		wanSet(func(s *wanState) { s.State, s.Msg, s.Secs = "fail", "ifup wan gagal", int(time.Since(t0).Seconds()) })
		return
	}
	wanSet(func(s *wanState) { s.Step = "Menunggu sambungan" })
	ok := false
	for i := 0; i < 90; i++ {
		if d := wanIface(); d != "" && ifaceIP(d) != "" {
			ok = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ok {
		wanSet(func(s *wanState) {
			s.State, s.Msg, s.Secs = "fail", "WAN tidak pulih dalam 90 saat", int(time.Since(t0).Seconds())
		})
		return
	}
	pub := ""
	for i := 0; i < 20 && pub == ""; i++ {
		if pub = directPub(); pub == "" {
			time.Sleep(2 * time.Second)
		}
	}
	nd := wanIface()
	wanSet(func(s *wanState) { s.Step = "Memulakan semula VLESS" })
	restartIfActive()
	wanSet(func(s *wanState) {
		s.State, s.Step, s.After, s.AfterPub, s.Secs = "done", "", ifaceIP(nd), pub, int(time.Since(t0).Seconds())
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
