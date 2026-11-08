package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const version = "1.1.17"

var home = func() string {
	if h := os.Getenv("VP_HOME"); h != "" {
		return h
	}
	return "/online/vlesspro"
}()

func printLic(l Lic) {
	fmt.Printf("id=%s\nstate=%s\nname=%s\nissued=%s\nexp=%s\n", l.ID, l.State, l.Name, l.Issued, l.Exp)
}

func main() {
	a := os.Args[1:]
	if len(a) == 0 {
		fmt.Println("vpro " + version + ": id | lic | activate KEY | node add|list|del|use | gen | serve")
		return
	}
	switch a[0] {
	case "id":
		fmt.Println(hyph(devID20()))
	case "pubfp":
		fmt.Println(pubFP())
	case "lic":
		if len(a) > 1 {
			printLic(checkKey(a[1]))
		} else {
			printLic(readLic())
		}
	case "activate":
		if len(a) < 2 {
			os.Exit(2)
		}
		l := checkKey(a[1])
		if l.State == "active" {
			_ = os.MkdirAll(home+"/etc", 0700)
			_ = os.WriteFile(home+"/etc/license", []byte(strings.Join(strings.Fields(a[1]), "")+"\n"), 0600)
		}
		printLic(l)
	case "node":
		nodeCLI(a[1:])
	case "gen":
		if err := genConfig(); err != nil {
			fmt.Fprintln(os.Stderr, "GAGAL:", err)
			os.Exit(1)
		}
	case "serve":
		serve()
	default:
		os.Exit(2)
	}
}

func nodeCLI(a []string) {
	if len(a) == 0 {
		return
	}
	switch a[0] {
	case "add":
		if len(a) < 2 {
			os.Exit(2)
		}
		p, err := parseLink(a[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "GAGAL:", err)
			os.Exit(1)
		}
		ns := append(loadNodes(), Node{Name: p.Name, Link: strings.TrimSpace(a[1])})
		_ = saveNodes(ns)
		fmt.Println("OK", len(ns)-1)
	case "list":
		for i, n := range loadNodes() {
			if p, err := parseLink(n.Link); err == nil {
				fmt.Printf("%d|%s|%s|%s|%s\n", i, p.Name, mask(p.Host), p.Net, orStr(p.Sec, "none"))
			}
		}
	}
}

func jr(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func pidAlive(p int) bool { return p > 0 && syscall.Kill(p, 0) == nil }

func readPid(f string) int {
	b, err := os.ReadFile(f)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// xliteDir ialah folder XLITE; B618 /online/xlite, AW1000 /opt/xlite.
var xliteDir = func() string {
	if d := os.Getenv("VP_XLITE"); d != "" {
		return d
	}
	return "/online/xlite"
}()

func xliteInstalled() bool {
	_, err := os.Stat(xliteDir + "/bin/xlite-core")
	return err == nil
}

func xliteRunning() bool {
	ms, _ := filepath.Glob("/proc/[0-9]*/exe")
	for _, m := range ms {
		if t, err := os.Readlink(m); err == nil && strings.HasPrefix(t, xliteDir+"/bin/xlite-core") {
			return true
		}
	}
	return false
}

func readStr(f, def string) string {
	b, err := os.ReadFile(f)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return def
	}
	return strings.TrimSpace(string(b))
}

func runVP(detach bool, args ...string) {
	cmd := exec.Command("/bin/sh", append([]string{home + "/vp"}, args...)...)
	lf, _ := os.OpenFile(home+"/run/vp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if lf != nil {
		cmd.Stdout, cmd.Stderr = lf, lf
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err == nil {
		go func() {
			_ = cmd.Wait()
			if lf != nil {
				lf.Close()
			}
		}()
	}
}

func tail(f string, n int64) string {
	fh, err := os.Open(f)
	if err != nil {
		return ""
	}
	defer fh.Close()
	st, _ := fh.Stat()
	if st.Size() > n {
		_, _ = fh.Seek(st.Size()-n, io.SeekStart)
	}
	b, _ := io.ReadAll(fh)
	return string(b)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
