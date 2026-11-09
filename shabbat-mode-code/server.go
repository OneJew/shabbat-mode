package main

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const port = "47813"

var address = "127.0.0.1:" + port

//go:embed ui.html
var uiHTML string

//go:embed alert.html
var alertHTML string

// alertSeen hears from the warning window that it opened.
var alertSeen = make(chan struct{}, 4)

//go:embed assets/emblem.png
var emblem []byte

//go:embed assets/logo-round.webp
var logo []byte

const version = "1.1.0"

func newToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type stateReply struct {
	Config    Config    `json:"config"`
	Cities    []City    `json:"cities"`
	Next      []Entry   `json:"next"`
	Now       time.Time `json:"now"`
	Autostart bool      `json:"autostart"`
	Error     string    `json:"error"`
}

// handler serves the settings page. Only this computer can reach it, and every
// change needs the per-run token, so other websites cannot change the settings.
func handler(e *Engine, token string, quit func()) http.Handler {
	mux := http.NewServeMux()
	fill := func(p string) string {
		p = strings.Replace(p, "__TOKEN__", token, 1)
		p = strings.ReplaceAll(p, "__EMBLEM__", "data:image/png;base64,"+base64.StdEncoding.EncodeToString(emblem))
		p = strings.ReplaceAll(p, "__LOGO__", "data:image/webp;base64,"+base64.StdEncoding.EncodeToString(logo))
		return strings.ReplaceAll(p, "__VERSION__", version)
	}
	page, alertPage := fill(uiHTML), fill(alertHTML)
	mux.HandleFunc("/alert", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Write([]byte(alertPage))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Write([]byte(page))
	})
	api := func(path string, f func(http.ResponseWriter, *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Token")), []byte(token)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			f(w, r)
		})
	}
	reply := func(w http.ResponseWriter) {
		cfg := e.config()
		e.mu.Lock()
		msg := e.lastErr
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stateReply{cfg, cities, entriesFrom(e.now(), cfg, 6), e.now(), autostartOn(), msg})
	}
	post := func(path string, f func(*http.Request) error) {
		api(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			if err := f(r); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			reply(w)
		})
	}
	api("/api/state", func(w http.ResponseWriter, r *http.Request) { reply(w) })
	post("/api/save", func(r *http.Request) error {
		var in struct {
			Config
			Autostart bool `json:"autostart"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4096)).Decode(&in); err != nil {
			return err
		}
		if !in.Config.valid() {
			return errInvalid
		}
		in.Config.SkipDate = e.config().SkipDate
		in.Config.Accepted = e.config().Accepted
		if err := setAutostart(in.Autostart); err != nil {
			return err
		}
		return e.setConfig(in.Config)
	})
	post("/api/skip", func(r *http.Request) error {
		var in struct {
			Date string `json:"date"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 256)).Decode(&in); err != nil {
			return err
		}
		return e.setSkip(in.Date)
	})
	post("/api/accept", func(r *http.Request) error {
		var in struct {
			City          string `json:"city"`
			CandleMinutes int    `json:"candleMinutes"`
			Autostart     bool   `json:"autostart"`
			Accepted      bool   `json:"accepted"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1024)).Decode(&in); err != nil {
			return err
		}
		c := e.config()
		c.City, c.CandleMinutes = findCity(in.City).Name, in.CandleMinutes
		if !in.Accepted || !c.valid() {
			return errInvalid
		}
		c.Accepted = true
		if err := setAutostart(in.Autostart); err != nil {
			return err
		}
		return e.setConfig(c)
	})
	post("/api/uninstall", func(r *http.Request) error {
		go func() {
			time.Sleep(300 * time.Millisecond)
			uninstall()
			quit()
		}()
		return nil
	})
	post("/api/test", func(r *http.Request) error {
		n := newNotice("בדיקה", "", e.config().Mode, e.now().Add(10*time.Minute), 10)
		n.Test = true
		n.Text = "זו בדיקה בלבד, והמחשב לא ייכבה.\n\nכך תיראה ההתראה לפני כניסת שבת או חג."
		go e.warn(n)
		return nil
	})
	post("/api/seen", func(r *http.Request) error {
		select {
		case alertSeen <- struct{}{}:
		default:
		}
		return nil
	})
	post("/api/quit", func(r *http.Request) error {
		go func() { time.Sleep(300 * time.Millisecond); quit() }()
		return nil
	})
	// Reject any request not addressed to our own local name (DNS rebinding).
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != address {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func main() {
	background := len(os.Args) > 1 && os.Args[1] == "--background"
	ln, err := net.Listen("tcp", address)
	replaced := false
	if err != nil {
		// Another copy is running. If it is a different version, close it and take its place,
		// so opening a new download always shows the new version.
		if !replaceRunning() {
			openMain()
			return
		}
		for i := 0; i < 40 && err != nil; i++ {
			time.Sleep(250 * time.Millisecond)
			ln, err = net.Listen("tcp", address)
		}
		if err != nil {
			openMain()
			return
		}
		replaced = true
	}
	if replaced && autostartOn() {
		setAutostart(true) // refresh the installed copy to this version
	}
	cfg := loadConfig()
	e := newEngine(cfg)
	go e.run()
	if !background {
		go func() { time.Sleep(200 * time.Millisecond); openMain() }()
	}
	startTray(e)
	srv := &http.Server{Handler: handler(e, newToken(), func() { removeTray(); os.Exit(0) }), ReadHeaderTimeout: 5 * time.Second}
	srv.Serve(ln)
}

func exitProgram() { os.Exit(0) }

func openMain() { openApp("http://"+address+"/", 620, 900, true) }

// showNotice shows the designed warning window. If it does not open within a few
// seconds, the plain Windows message appears instead, so a warning is never lost.
func showNotice(n notice) bool {
	for len(alertSeen) > 0 {
		<-alertSeen
	}
	q := url.Values{}
	q.Set("n", n.Name)
	q.Set("d", n.Date)
	q.Set("m", n.Mode)
	q.Set("at", strconv.FormatInt(n.At.UnixMilli(), 10))
	if n.Test {
		q.Set("t", "1")
	}
	if openApp("http://"+address+"/alert?"+q.Encode(), 480, 620, false) {
		select {
		case <-alertSeen:
			return false
		case <-time.After(15 * time.Second):
		}
	}
	return messageBox(n.Text)
}

var (
	tokenRe   = regexp.MustCompile(`var TOKEN = "([0-9a-f]+)"`)
	versionRe = regexp.MustCompile(`גרסה ([0-9]+\.[0-9]+\.[0-9]+)`)
)

// replaceRunning asks a running copy of another version to close. It reads the running
// copy's page like any local window would, and uses its own close button.
func replaceRunning() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://" + address + "/")
	if err != nil {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	res.Body.Close()
	if v := versionRe.FindSubmatch(body); v != nil && string(v[1]) == version {
		return false
	}
	t := tokenRe.FindSubmatch(body)
	if t == nil {
		return false
	}
	req, _ := http.NewRequest(http.MethodPost, "http://"+address+"/api/quit", strings.NewReader("{}"))
	req.Header.Set("X-Token", string(t[1]))
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}
