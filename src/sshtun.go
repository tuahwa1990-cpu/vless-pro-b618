package main

// Terowong SSH (WebSocket + payload, SSL/TLS stunnel, atau SSL + payload).
// Untuk setiap nod SSH dalam pilihan aktif, vpro membuka SOCKS5 di 127.0.0.1:<sshPort(i)>.
// Xray menghantar trafik ke SOCKS itu; setiap sambungan dibuka sebagai saluran direct-tcpip SSH.

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const defPayload = "GET / HTTP/1.1[crlf]Host: [host][crlf]Upgrade: websocket[crlf]Connection: Upgrade[crlf][crlf]"

// expandPayload menggantikan penanda biasa (gaya HTTP Custom/Injector). [split] memisahkan bahagian yang dihantar berasingan.
func expandPayload(pl, host string, port int) []string {
	if strings.TrimSpace(pl) == "" {
		pl = defPayload
	}
	hp := net.JoinHostPort(host, strconv.Itoa(port))
	r := strings.NewReplacer(
		"[crlf]", "\r\n", "[CRLF]", "\r\n", "[cr]", "\r", "[lf]", "\n", "\\r", "\r", "\\n", "\n",
		"[host_port]", hp, "[host]", host, "[port]", strconv.Itoa(port),
		"[protocol]", "HTTP/1.1", "[ua]", "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36",
		"[raw]", "CONNECT "+hp+" HTTP/1.1",
	)
	pl = r.Replace(pl)
	var parts []string
	for _, s := range strings.Split(strings.ReplaceAll(pl, "[delay_split]", "[split]"), "[split]") {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(b []byte) (int, error) { return c.r.Read(b) }

// skipChunkedBody menghabiskan badan HTTP "Transfer-Encoding: chunked" tanpa menyimpannya.
func skipChunkedBody(br *bufio.Reader) error {
	for {
		sizeLine, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		sizeLine = strings.TrimSpace(strings.SplitN(sizeLine, ";", 2)[0])
		sz, err := strconv.ParseInt(sizeLine, 16, 64)
		if err != nil {
			return err
		}
		if sz == 0 {
			for {
				h, err := br.ReadString('\n')
				if err != nil {
					return err
				}
				if strings.TrimSpace(h) == "" {
					return nil
				}
			}
		}
		if _, err := io.CopyN(io.Discard, br, sz); err != nil {
			return err
		}
		if _, err := br.ReadString('\n'); err != nil { // CRLF selepas chunk
			return err
		}
	}
}

// readHTTPReplies membaca `n` blok respons HTTP selepas payload (satu bagi setiap bahagian
// payload yang dihantar); terima 101 atau 2xx. Badan respons (ikut Content-Length atau
// chunked) dihabiskan supaya tidak tertinggal dalam buffer dan tersalah anggap sebagai
// data terowong mentah (punca ralat "ssh: overflow reading version string").
func readHTTPReplies(br *bufio.Reader, n int) error {
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		line, err := br.ReadString('\n')
		if err != nil {
			return errors.New("pelayan tidak membalas payload")
		}
		f := strings.Fields(line)
		if len(f) < 2 || !strings.HasPrefix(f[0], "HTTP/") {
			return errors.New("balasan pelayan bukan HTTP: " + strings.TrimSpace(line))
		}
		code, _ := strconv.Atoi(f[1])
		contentLen := -1
		chunked := false
		for {
			h, err := br.ReadString('\n')
			if err != nil {
				return errors.New("balasan HTTP terputus")
			}
			if strings.TrimSpace(h) == "" {
				break
			}
			hl := strings.ToLower(h)
			switch {
			case strings.HasPrefix(hl, "content-length:"):
				if v, e := strconv.Atoi(strings.TrimSpace(h[len("content-length:"):])); e == nil {
					contentLen = v
				}
			case strings.HasPrefix(hl, "transfer-encoding:") && strings.Contains(hl, "chunked"):
				chunked = true
			}
		}
		if code != 101 && (code < 200 || code > 299) {
			return fmt.Errorf("pelayan menolak payload (HTTP %d)", code)
		}
		if code == 101 {
			// Peralihan protokol — tiada badan; baki aliran ialah data mentah terowong.
			return nil
		}
		if chunked {
			if err := skipChunkedBody(br); err != nil {
				return errors.New("badan HTTP (chunked) tidak lengkap")
			}
		} else if contentLen > 0 {
			if _, err := io.CopyN(io.Discard, br, int64(contentLen)); err != nil {
				return errors.New("badan HTTP tidak lengkap")
			}
		}
	}
	return nil
}

func sshDialRaw(p *Parsed) (net.Conn, error) {
	c := p.SSH
	target := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	if c.Proxy != "" {
		target = c.Proxy
	}
	conn, err := net.DialTimeout("tcp", target, 15*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
	if c.Mode == "tls" || c.Mode == "tlsws" {
		sni := p.SNI
		if sni == "" {
			sni = p.Host
		}
		tc := tls.Client(conn, &tls.Config{ServerName: sni, InsecureSkipVerify: true})
		if err := tc.Handshake(); err != nil {
			conn.Close()
			return nil, errors.New("SSL gagal: " + shortErr(err))
		}
		conn = tc
	}
	var out net.Conn = conn
	if c.Mode == "ws" || c.Mode == "tlsws" {
		parts := expandPayload(c.Payload, p.Host, p.Port)
		for i, part := range parts {
			if i > 0 {
				time.Sleep(200 * time.Millisecond)
			}
			if _, err := io.WriteString(conn, part); err != nil {
				conn.Close()
				return nil, err
			}
		}
		br := bufio.NewReader(conn)
		if err := readHTTPReplies(br, len(parts)); err != nil {
			conn.Close()
			return nil, err
		}
		out = &bufConn{Conn: conn, r: br}
	}
	return out, nil
}

func sshConnect(p *Parsed) (*ssh.Client, error) {
	raw, err := sshDialRaw(p)
	if err != nil {
		return nil, err
	}
	pass := p.SSH.Pass
	cfg := &ssh.ClientConfig{
		User: p.SSH.User,
		Auth: []ssh.AuthMethod{ssh.Password(pass), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			a := make([]string, len(qs))
			for i := range a {
				a[i] = pass
			}
			return a, nil
		})},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	}
	cc, chans, reqs, err := ssh.NewClientConn(raw, net.JoinHostPort(p.Host, strconv.Itoa(p.Port)), cfg)
	if err != nil {
		raw.Close()
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, errors.New("nama pengguna atau kata laluan SSH salah")
		}
		return nil, errors.New("SSH gagal: " + shortErr(err))
	}
	_ = raw.SetDeadline(time.Time{})
	return ssh.NewClient(cc, chans, reqs), nil
}

type sshTun struct {
	link string
	p    *Parsed
	port int
	ln   net.Listener
	mu   sync.Mutex
	cl   *ssh.Client
	fail time.Time
	msg  string
	quit chan struct{}
}

func (t *sshTun) client() (*ssh.Client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cl != nil {
		return t.cl, nil
	}
	if time.Since(t.fail) < 3*time.Second {
		return nil, errors.New(t.msg)
	}
	cl, err := sshConnect(t.p)
	if err != nil {
		t.fail, t.msg = time.Now(), err.Error()
		logf("SSH %s: %s", t.p.Name, t.msg)
		return nil, err
	}
	t.cl, t.msg = cl, "bersambung"
	logf("SSH %s: bersambung", t.p.Name)
	go t.keepalive(cl)
	return cl, nil
}

func (t *sshTun) drop(cl *ssh.Client) {
	t.mu.Lock()
	if t.cl == cl {
		t.cl = nil
	}
	t.mu.Unlock()
	cl.Close()
}

func (t *sshTun) keepalive(cl *ssh.Client) {
	for {
		select {
		case <-t.quit:
			return
		case <-time.After(25 * time.Second):
		}
		done := make(chan error, 1)
		go func() { _, _, err := cl.SendRequest("keepalive@openssh.com", true, nil); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				t.drop(cl)
				return
			}
		case <-time.After(15 * time.Second):
			t.drop(cl)
			return
		}
	}
}

func (t *sshTun) dial(addr string) (net.Conn, error) {
	for try := 0; try < 2; try++ {
		cl, err := t.client()
		if err != nil {
			return nil, err
		}
		c, err := cl.Dial("tcp", addr)
		if err == nil {
			return c, nil
		}
		// saluran ditolak (contohnya hos tidak wujud) tidak bermakna sambungan SSH mati
		var oe *ssh.OpenChannelError
		if errors.As(err, &oe) {
			return nil, err
		}
		t.drop(cl)
	}
	return nil, errors.New("sambungan SSH terputus")
}

func (t *sshTun) serve() {
	for {
		c, err := t.ln.Accept()
		if err != nil {
			return
		}
		go t.handle(c)
	}
}

// handle: pelayan SOCKS5 ringkas (CONNECT sahaja, tanpa pengesahan).
func (t *sshTun) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	h := make([]byte, 2)
	if _, err := io.ReadFull(c, h); err != nil || h[0] != 5 {
		return
	}
	if _, err := io.ReadFull(c, make([]byte, int(h[1]))); err != nil {
		return
	}
	_, _ = c.Write([]byte{5, 0})
	q := make([]byte, 4)
	if _, err := io.ReadFull(c, q); err != nil {
		return
	}
	reply := func(code byte) { _, _ = c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0}) }
	if q[1] != 1 {
		reply(7)
		return
	}
	var host string
	switch q[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = string(b)
	default:
		reply(8)
		return
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return
	}
	addr := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb))))
	rc, err := t.dial(addr)
	if err != nil {
		reply(5)
		return
	}
	defer rc.Close()
	reply(0)
	_ = c.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(rc, c); rc.Close(); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, rc); c.Close(); done <- struct{}{} }()
	<-done
}

func (t *sshTun) close() {
	close(t.quit)
	t.ln.Close()
	t.mu.Lock()
	if t.cl != nil {
		t.cl.Close()
		t.cl = nil
	}
	t.mu.Unlock()
}

func startTun(p *Parsed, link string, port int) (*sshTun, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	t := &sshTun{link: link, p: p, port: port, ln: ln, quit: make(chan struct{}), msg: "menunggu"}
	go t.serve()
	return t, nil
}

var (
	tunMu sync.Mutex
	tuns  = map[int]*sshTun{}
)

// wantedTuns: nod SSH dalam pilihan aktif (port sama seperti dalam konfigurasi Xray).
func wantedTuns() map[int]*Parsed {
	want := map[int]*Parsed{}
	if readStr(home+"/etc/mode", "xlite") != "vless" {
		return want
	}
	ps, _, err := activeParsed()
	if err != nil {
		return want
	}
	for i, p := range ps {
		if p.Proto == "ssh" {
			want[sshPort(i)] = p
		}
	}
	return want
}

func tunKey(p *Parsed) string {
	return fmt.Sprintf("%s|%d|%s|%s|%s|%s|%s|%s", p.Host, p.Port, p.SSH.User, p.SSH.Pass, p.SSH.Mode, p.SNI, p.SSH.Payload, p.SSH.Proxy)
}

func syncTuns() {
	want := wantedTuns()
	tunMu.Lock()
	defer tunMu.Unlock()
	for port, t := range tuns {
		if p, ok := want[port]; !ok || tunKey(p) != t.link {
			t.close()
			delete(tuns, port)
		}
	}
	for port, p := range want {
		if _, ok := tuns[port]; ok {
			continue
		}
		t, err := startTun(p, tunKey(p), port)
		if err != nil {
			logf("SSH: port %d tidak dapat dibuka: %v", port, err)
			continue
		}
		tuns[port] = t
	}
}

// sshLoop menyelaraskan terowong SSH dengan mod dan pilihan nod semasa.
func sshLoop() {
	for {
		syncTuns()
		time.Sleep(2 * time.Second)
	}
}

// sshStatus: keadaan terowong untuk paparan.
func sshStatus() []map[string]any {
	tunMu.Lock()
	defer tunMu.Unlock()
	var out []map[string]any
	for port, t := range tuns {
		t.mu.Lock()
		out = append(out, map[string]any{"name": t.p.Name, "port": port, "up": t.cl != nil, "msg": t.msg})
		t.mu.Unlock()
	}
	return out
}

// logf menulis satu baris ke run/vp.log (dipaparkan di halaman Log).
func logf(f string, a ...any) {
	lf, err := os.OpenFile(home+"/run/vp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer lf.Close()
	fmt.Fprintf(lf, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(f, a...))
}
