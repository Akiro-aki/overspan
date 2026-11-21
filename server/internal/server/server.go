// Package server is the Overspan display server: it serves the browser
// viewer to the LAN, the dashboard to localhost, and runs one session per
// connected viewer.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"overspan/internal/input"
	"overspan/internal/qr"
	"overspan/internal/vdisplay"
)

const Version = "0.1.0"

// Server holds all shared state.
type Server struct {
	Port    int
	Web     fs.FS
	cfgPath string

	mu       sync.Mutex
	settings Settings
	viewers  map[string]*Viewer // by session id
	kept     map[string]*keptDisplay
	backend  vdisplay.Backend
	note     string
	inj      *input.Injector
	injErr   error
	adminKey string
	logs     []LogLine
	subs     map[chan struct{}]bool
	layout   []vdisplay.Output
	layoutAt time.Time
	closing  bool
	sessions sync.WaitGroup // one per viewer connection, so Close can wait for cleanup
}

// LogLine is one entry of the dashboard's activity log.
type LogLine struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

type keptDisplay struct {
	output string
	spec   vdisplay.Spec
	timer  *time.Timer
}

// New prepares a server. backendName "" or "auto" detects the desktop.
func New(port int, web fs.FS, backendName string) (*Server, error) {
	s := &Server{
		Port:     port,
		Web:      web,
		cfgPath:  configPath(),
		viewers:  map[string]*Viewer{},
		kept:     map[string]*keptDisplay{},
		subs:     map[chan struct{}]bool{},
		adminKey: randomHex(16),
	}
	s.settings = loadSettings(s.cfgPath)
	if backendName == "" || backendName == "auto" {
		s.backend, s.note = vdisplay.Detect()
	} else {
		b, err := vdisplay.ByName(backendName)
		if err != nil {
			return nil, err
		}
		s.backend, s.note = b, "chosen with --backend"
	}
	s.logf("info", "Display backend: %s (%s)", s.backend.Name(), s.note)
	s.inj, s.injErr = input.Open()
	if s.injErr != nil {
		s.logf("warn", "Input from viewers is off: %v", s.injErr)
	}
	return s, nil
}

// Close removes every virtual monitor this server made.
func (s *Server) Close() {
	s.mu.Lock()
	s.closing = true // sessions now remove their monitors at once instead of keeping them
	viewers := make([]*Viewer, 0, len(s.viewers))
	for _, v := range s.viewers {
		viewers = append(viewers, v)
	}
	kept := s.kept
	s.kept = map[string]*keptDisplay{}
	s.mu.Unlock()
	for _, v := range viewers {
		v.stop("server shutting down")
	}
	for _, k := range kept {
		k.timer.Stop()
		s.backend.Remove(k.output)
	}
	// Wait for every session to finish its cleanup, including one that is
	// halfway through creating a monitor, but never hang shutdown forever.
	done := make(chan struct{})
	go func() { s.sessions.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		log.Printf("[warn] some sessions did not finish; check for leftover OVERSPAN monitors")
	}
	if s.inj != nil {
		s.inj.Close()
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) logf(level, format string, args ...any) {
	line := LogLine{At: time.Now(), Level: level, Text: fmt.Sprintf(format, args...)}
	log.Printf("[%s] %s", level, line.Text)
	s.mu.Lock()
	s.logs = append(s.logs, line)
	if len(s.logs) > 200 {
		s.logs = s.logs[len(s.logs)-200:]
	}
	s.mu.Unlock()
	s.notify()
}

// notify wakes every dashboard event stream.
func (s *Server) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Layout returns the cached global layout, refreshed at most every 2s.
func (s *Server) Layout(force bool) []vdisplay.Output {
	s.mu.Lock()
	if !force && time.Since(s.layoutAt) < 2*time.Second && s.layout != nil {
		l := s.layout
		s.mu.Unlock()
		return l
	}
	s.mu.Unlock()
	outs, err := s.backend.Outputs()
	if err != nil {
		return s.layout
	}
	s.mu.Lock()
	s.layout, s.layoutAt = outs, time.Now()
	s.mu.Unlock()
	return outs
}

// Handler builds the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static := http.FileServer(http.FS(s.Web))

	// LAN-facing: the viewer
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, s.Web, "viewer.html")
	})
	mux.Handle("GET /assets/", static)
	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		serveFile(w, r, s.Web, "manifest.webmanifest")
	})
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /ws", s.handleViewer)

	// localhost-only: the dashboard
	mux.HandleFunc("GET /admin", s.adminOnly(false, func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(s.Web, "admin.html")
		if err != nil {
			http.Error(w, "dashboard missing", 500)
			return
		}
		page := strings.Replace(string(b), "__ADMIN_KEY__", s.adminKey, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(page))
	}))
	mux.HandleFunc("GET /api/state", s.adminOnly(true, s.handleState))
	mux.HandleFunc("GET /api/events", s.adminOnly(true, s.handleEvents))
	mux.HandleFunc("POST /api/settings", s.adminOnly(true, s.handleSettings))
	mux.HandleFunc("POST /api/viewers/{id}/{action}", s.adminOnly(true, s.handleViewerAction))
	mux.HandleFunc("POST /api/trusted/{device}/forget", s.adminOnly(true, s.handleForget))
	mux.HandleFunc("GET /api/doctor", s.adminOnly(true, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.Doctor())
	}))

	return securityHeaders(mux)
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:")
		h.ServeHTTP(w, r)
	})
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// adminOnly guards the dashboard: requests must come from this machine, name
// a local Host (blocks DNS rebinding), and API calls must carry the key that
// is only ever written into the page served to localhost (blocks CSRF).
func (s *Server) adminOnly(api bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hh, _, err := net.SplitHostPort(host); err == nil {
			host = hh
		}
		localHost := host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
		if !isLoopback(r) || !localHost {
			http.Error(w, "The dashboard is only available on the computer running Overspan: open http://localhost:"+fmt.Sprint(s.Port)+"/admin there.", http.StatusForbidden)
			return
		}
		if api {
			key := r.Header.Get("X-Overspan-Key")
			if key == "" {
				key = r.URL.Query().Get("key") // EventSource cannot set headers
			}
			if key != s.adminKey {
				http.Error(w, "stale dashboard: reload the page", http.StatusUnauthorized)
				return
			}
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// Addresses lists the URLs viewers on the LAN can open.
func (s *Server) Addresses() []string {
	var urls []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := ifc.Name
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "virbr") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			urls = append(urls, fmt.Sprintf("http://%s:%d/", ipn.IP, s.Port))
		}
	}
	return urls
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	st := s.settings
	s.mu.Unlock()
	writeJSON(w, map[string]any{
		"product":   "Overspan",
		"version":   Version,
		"name":      st.ServerName,
		"enabled":   st.Enabled,
		"pin":       st.PIN != "",
		"canExtend": s.backend.CanCreate(),
		"mode":      st.DefaultMode,
		"input":     st.AllowInput && s.inj != nil,
	})
}

type stateDTO struct {
	Settings  Settings          `json:"settings"`
	Viewers   []ViewerDTO       `json:"viewers"`
	Addresses []string          `json:"addresses"`
	QR        string            `json:"qr"`
	Backend   string            `json:"backend"`
	Note      string            `json:"note"`
	CanExtend bool              `json:"canExtend"`
	Input     string            `json:"input"`
	Outputs   []vdisplay.Output `json:"outputs"`
	Logs      []LogLine         `json:"logs"`
	Version   string            `json:"version"`
}

func (s *Server) state() stateDTO {
	addrs := s.Addresses()
	qrSVG := ""
	if len(addrs) > 0 {
		if c, err := qr.Encode(addrs[0]); err == nil {
			qrSVG = c.SVG()
		}
	}
	outs := s.Layout(false)
	s.mu.Lock()
	defer s.mu.Unlock()
	st := stateDTO{
		Settings: s.settings, Addresses: addrs, QR: qrSVG, Backend: s.backend.Name(),
		Note: s.note, CanExtend: s.backend.CanCreate(), Outputs: outs, Version: Version,
		Logs: append([]LogLine(nil), s.logs...),
	}
	st.Input = "ok"
	if s.injErr != nil {
		st.Input = s.injErr.Error()
	}
	for _, v := range s.viewers {
		st.Viewers = append(st.Viewers, v.dto())
	}
	if st.Viewers == nil {
		st.Viewers = []ViewerDTO{}
	}
	return st
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.state()) }

// handleEvents streams the full state whenever it changes (Server-Sent
// Events), plus a heartbeat every second so stats stay live.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[ch] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	send := func() bool {
		b, _ := json.Marshal(s.state())
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send() {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
		case <-tick.C:
		}
		if !send() {
			return
		}
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	next := s.settings
	s.mu.Unlock()
	trusted := next.Trusted
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&next); err != nil {
		http.Error(w, "bad settings: "+err.Error(), 400)
		return
	}
	next.Trusted = trusted // only changed through approve / forget
	next.sanitize()
	s.mu.Lock()
	was := s.settings.Enabled
	s.settings = next
	err := saveSettings(s.cfgPath, next)
	viewers := s.viewerList()
	s.mu.Unlock()
	if err != nil {
		s.logf("error", "Could not save settings: %v", err)
	}
	if was && !next.Enabled {
		for _, v := range viewers {
			v.stop("The server stopped accepting viewers.")
		}
		s.logf("info", "Server paused: new viewers are refused")
	} else if !was && next.Enabled {
		s.logf("info", "Server is accepting viewers again")
	}
	for _, v := range viewers {
		v.applySettings(next)
	}
	s.notify()
	writeJSON(w, next)
}

func (s *Server) viewerList() []*Viewer {
	out := make([]*Viewer, 0, len(s.viewers))
	for _, v := range s.viewers {
		out = append(out, v)
	}
	return out
}

func (s *Server) handleViewerAction(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	v := s.viewers[r.PathValue("id")]
	s.mu.Unlock()
	if v == nil {
		http.Error(w, "that viewer has already left", 404)
		return
	}
	switch r.PathValue("action") {
	case "approve":
		remember := r.URL.Query().Get("remember") == "1"
		if remember {
			s.trust(v.DeviceID, v.Name)
		}
		v.decide(true)
	case "deny":
		v.decide(false)
	case "disconnect":
		v.stop("Disconnected from the server's dashboard.")
	default:
		http.Error(w, "unknown action", 400)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) trust(device, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.settings.Trusted {
		if t.DeviceID == device {
			s.settings.Trusted[i].Name = name
			s.settings.Trusted[i].LastSeen = time.Now()
			saveSettings(s.cfgPath, s.settings)
			return
		}
	}
	s.settings.Trusted = append(s.settings.Trusted, Trusted{DeviceID: device, Name: name, Added: time.Now(), LastSeen: time.Now()})
	saveSettings(s.cfgPath, s.settings)
}

func (s *Server) isTrusted(device string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.settings.Trusted {
		if t.DeviceID == device {
			s.settings.Trusted[i].LastSeen = time.Now()
			return true
		}
	}
	return false
}

func (s *Server) handleForget(w http.ResponseWriter, r *http.Request) {
	dev := r.PathValue("device")
	s.mu.Lock()
	kept := s.settings.Trusted[:0]
	for _, t := range s.settings.Trusted {
		if t.DeviceID != dev {
			kept = append(kept, t)
		}
	}
	s.settings.Trusted = kept
	saveSettings(s.cfgPath, s.settings)
	s.mu.Unlock()
	s.notify()
	writeJSON(w, map[string]bool{"ok": true})
}
