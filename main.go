// Rigscope shows what's inside a Windows PC, what it can run, and whether it's secure.
//
// It is a single executable. It starts a small web server that only listens on
// 127.0.0.1, opens the interface in a Microsoft Edge (or Chrome) app window,
// and quits when that window closes. It only reads information: it never
// changes settings and never sends anything over the internet.
package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

var version = "1.1.0" // set at build time with -ldflags "-X main.version=..."

//go:embed ui/index.html
var indexHTML string

//go:embed ui/live.js
var liveJS string

//go:embed ui/live.css
var liveCSS string

//go:embed ps/specs.ps1
var specsScript string

var (
	token    string
	addr     string
	clients  atomic.Int64
	lastSeen atomic.Int64
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Println("Rigscope " + version)
			return
		case "--live":
			// Print one live sample (load, temperatures, top apps) as JSON and exit.
			mon.get()
			time.Sleep(1500 * time.Millisecond)
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(sampleLive())
			return
		case "--json":
			// Print the full report and exit. Handy for bug reports and scripts.
			r, err := getReport(true)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(r)
			return
		}
	}

	listen := os.Getenv("RIGSCOPE_ADDR")
	if listen == "" {
		listen = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		showFatal("Rigscope could not start: " + err.Error())
		os.Exit(1)
	}
	addr = ln.Addr().String()
	if t := os.Getenv("RIGSCOPE_TOKEN"); t != "" {
		token = t
	} else {
		b := make([]byte, 16)
		rand.Read(b)
		token = hex.EncodeToString(b)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		rep, err := getReport(r.URL.Query().Get("refresh") == "1")
		if err != nil {
			fail(w, 500, "Rigscope couldn't read this PC's details: "+err.Error())
			return
		}
		writeJSON(w, rep)
	})
	mux.HandleFunc("/api/live", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, mon.get()) })
	mux.HandleFunc("/live.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, liveJS)
	})
	mux.HandleFunc("/live.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		fmt.Fprint(w, liveCSS)
	})
	mux.HandleFunc("/api/elevate", handleElevate)
	mux.HandleFunc("/api/events", handleEvents)

	go getReport(false) // start collecting while the window opens
	go http.Serve(ln, guard(mux))
	url := "http://" + addr + "/"
	lastSeen.Store(time.Now().Unix())
	if os.Getenv("RIGSCOPE_NO_WINDOW") == "" {
		openWindow(url)
	} else {
		fmt.Println(url)
	}
	watchdog()
}

// watchdog quits shortly after the app window is closed.
func watchdog() {
	keep := os.Getenv("RIGSCOPE_KEEPALIVE") != ""
	seen := false
	for {
		time.Sleep(2 * time.Second)
		if keep {
			continue
		}
		if clients.Load() > 0 {
			seen = true
			lastSeen.Store(time.Now().Unix())
			continue
		}
		idle := time.Now().Unix() - lastSeen.Load()
		if (seen && idle > 8) || (!seen && idle > 120) {
			os.Exit(0)
		}
	}
}

// guard only lets Rigscope's own window use the API. The random token stops other
// websites from reading the report, and the Host check blocks DNS rebinding.
func guard(next http.Handler) http.Handler {
	_, port, _ := net.SplitHostPort(addr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr && r.Host != "localhost:"+port {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			t := r.Header.Get("X-Token")
			if r.URL.Path == "/api/events" {
				t = r.URL.Query().Get("t")
			}
			if t != token {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// No outside scripts, styles, fonts or images: everything the page needs is inside this file.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
	fmt.Fprint(w, strings.Replace(indexHTML, "__TOKEN__", token, 1))
}

func handleElevate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "Use POST")
		return
	}
	if err := elevate(); err != nil {
		fail(w, 403, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
	go func() { time.Sleep(800 * time.Millisecond); os.Exit(0) }()
}

func handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	clients.Add(1)
	defer func() { clients.Add(-1); lastSeen.Store(time.Now().Unix()) }()
	fmt.Fprint(w, "retry: 1000\n\n")
	fl.Flush()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}
