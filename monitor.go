package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Live monitoring.
//
// A background loop samples the PC once a second while Rigscope is open:
// per-core load, clock speed, throttling, temperatures, memory, disk and network
// throughput, graphics load and every running process. The window polls
// /api/live for the latest sample. Platform code fills in a rawLive; this file
// turns it into what the window shows.

type Live struct {
	Time     int64     `json:"t"` // unix milliseconds
	CPU      LiveCPU   `json:"cpu"`
	Temp     LiveTemp  `json:"temp"`
	Mem      LiveMem   `json:"mem"`
	GPU      LiveGPU   `json:"gpu"`
	Disk     LiveIO    `json:"disk"`
	Net      LiveIO    `json:"net"`
	Power    LivePower `json:"power"`
	Procs    []Group   `json:"procs"`
	NProcs   int       `json:"nProcs"`
	Threads  int       `json:"threads"`
	UptimeS  int64     `json:"uptime"`
	Sources  []string  `json:"sources"` // where readings come from, for the window's notes
	Warnings []string  `json:"warnings"`
}

type LiveCPU struct {
	Total   float64   `json:"total"`   // percent of the whole processor
	Cores   []float64 `json:"cores"`   // percent per logical processor
	MHz     float64   `json:"mhz"`     // current average speed, 0 if unknown
	BaseMHz float64   `json:"baseMhz"` // rated base speed
	Limit   float64   `json:"limit"`   // 100 = running unrestricted; lower means throttled, -1 unknown
	Power   float64   `json:"power"`   // package watts from a sensor app, -1 unknown
}

type Reading struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type LiveTemp struct {
	CPU    float64   `json:"cpu"`    // °C, -1 if unavailable
	Source string    `json:"source"` // sensor, thermalzone or none
	Cores  []Reading `json:"cores"`  // per-core temperatures when a sensor app provides them
	Zones  []Reading `json:"zones"`  // ACPI thermal zones
	GPU    float64   `json:"gpu"`    // °C, -1 if unavailable
	Fans   []Reading `json:"fans"`   // RPM
	Other  []Reading `json:"other"`  // drives, motherboard and so on
}

type LiveMem struct {
	Total       int64 `json:"total"`
	Used        int64 `json:"used"`
	Committed   int64 `json:"committed"`
	CommitLimit int64 `json:"commitLimit"`
}

type LiveGPU struct {
	Name      string  `json:"name"`
	Util      float64 `json:"util"`      // percent, -1 unknown
	VRAMUsed  int64   `json:"vramUsed"`  // bytes, -1 unknown
	VRAMTotal int64   `json:"vramTotal"` // bytes, 0 unknown
	Shared    bool    `json:"shared"`    // built-in graphics using system memory
	Power     float64 `json:"power"`     // watts, -1 unknown
	FanPct    float64 `json:"fanPct"`    // percent, -1 unknown
	ClockMHz  float64 `json:"clockMhz"`  // -1 unknown
	Source    string  `json:"source"`
}

type LiveIO struct {
	In     float64 `json:"in"`     // bytes per second read or received
	Out    float64 `json:"out"`    // bytes per second written or sent
	Active float64 `json:"active"` // disk busy percent, -1 unknown
	OK     bool    `json:"ok"`     // false if Windows didn't provide this counter
}

type LivePower struct {
	PowerStatus
	Watts float64 `json:"watts"` // battery charge (+) or discharge (-) rate, 0 unknown
}

type Group struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Desc  string  `json:"desc"`
	Note  string  `json:"note"`
	Kind  string  `json:"kind"` // app, background, windows, self
	Count int     `json:"count"`
	CPU   float64 `json:"cpu"`
	Mem   int64   `json:"mem"`
	IO    float64 `json:"io"`
	GPU   float64 `json:"gpu"`
}

// rawProc is one process from the platform sampler.
type rawProc struct {
	PID, PPID uint32
	Name      string // image name, like chrome.exe
	Path      string
	Desc      string // file description from the exe's version info
	CPU       float64
	Mem       int64
	IO        float64
	GPU       float64
	Windowed  bool
	System    bool // part of Windows
}

var notes = map[string]string{
	"msmpeng.exe":               "Microsoft Defender scanning for threats. It usually settles down after a few minutes.",
	"mpdefendercoreservice.exe": "Part of Microsoft Defender.",
	"dwm.exe":                   "Draws windows, animations and effects on your screen.",
	"system":                    "Windows itself and its drivers.",
	"memory compression":        "Windows squeezing memory so more fits in RAM. A large number here means RAM is running short.",
	"svchost.exe":               "Runs Windows services. Seeing many of these is normal.",
	"searchindexer.exe":         "Indexing files so Windows search is fast. Busy for a while after big changes.",
	"searchhost.exe":            "The Windows search box.",
	"msedgewebview2.exe":        "Web content shown inside other apps, such as Widgets, Teams, Outlook or the Start menu.",
	"explorer.exe":              "The taskbar, Start menu and File Explorer.",
	"tiworker.exe":              "Installing Windows updates. Heavy for a while, then it stops.",
	"trustedinstaller.exe":      "Installing Windows updates.",
	"vmmem":                     "Memory and processor used by WSL, Docker or a virtual machine.",
	"vmmemwsl":                  "Memory and processor used by WSL or Docker.",
	"onedrive.exe":              "Syncing your files with OneDrive.",
	"audiodg.exe":               "Windows audio processing.",
	"csrss.exe":                 "Core Windows process. Never end it.",
	"lsass.exe":                 "Handles sign-in and security. Never end it.",
	"wininit.exe":               "Core Windows process. Never end it.",
	"services.exe":              "Starts and stops Windows services.",
	"compattelrunner.exe":       "Windows compatibility checks. Runs now and then.",
	"wmiprvse.exe":              "Answers questions about the PC for apps, including Rigscope.",
	"nvcontainer.exe":           "NVIDIA driver services.",
	"nvdisplay.container.exe":   "NVIDIA display driver service.",
	"registry":                  "Holds the Windows registry in memory.",
	"phoneexperiencehost.exe":   "Phone Link, connecting your phone to Windows.",
	"widgets.exe":               "The Widgets board.",
	"spoolsv.exe":               "Printing.",
	"ctfmon.exe":                "Keyboard input and language switching.",
}

var friendly = map[string]string{
	"system": "System", "memory compression": "Memory Compression", "registry": "Registry",
	"secure system": "Secure System", "vmmem": "Virtual machines (vmmem)", "vmmemwsl": "WSL and Docker (vmmemwsl)",
}

// groupProcs merges processes with the same program into one row, like Task Manager does.
func groupProcs(procs []rawProc, self map[uint32]bool, ncpu int) []Group {
	groups := map[string]*Group{}
	for _, p := range procs {
		key := strings.ToLower(p.Name)
		kind := "background"
		switch {
		case self[p.PID]:
			key, kind = "#rigscope", "self"
		case p.System:
			kind = "windows"
		case p.Windowed:
			kind = "app"
		}
		g := groups[key]
		if g == nil {
			name := friendly[key]
			if name == "" {
				name = p.Desc
			}
			if name == "" || strings.EqualFold(name, p.Name) {
				name = prettyExe(p.Name)
			}
			g = &Group{Key: key, Name: name, Desc: p.Name, Note: notes[key], Kind: kind}
			if kind == "self" {
				g.Name, g.Desc, g.Note = "Rigscope", "Rigscope.exe and its window", "This app, including the window you're looking at."
			}
			groups[key] = g
		}
		if kind == "app" && g.Kind == "background" {
			g.Kind = "app"
		}
		g.Count++
		g.CPU += p.CPU
		g.Mem += p.Mem
		g.IO += p.IO
		g.GPU += p.GPU
	}
	out := make([]Group, 0, len(groups))
	for _, g := range groups {
		if g.CPU > 100 {
			g.CPU = 100
		}
		if g.GPU > 100 {
			g.GPU = 100
		}
		g.CPU, g.GPU = round1(g.CPU), round1(g.GPU)
		g.IO = float64(int64(g.IO))
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		return out[i].Mem > out[j].Mem
	})
	return out
}

func prettyExe(n string) string {
	n = strings.TrimSuffix(strings.TrimSuffix(n, ".exe"), ".EXE")
	if n == "" {
		return "Unknown"
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

// descendants finds every process started by root, directly or indirectly.
func descendants(procs []rawProc, root uint32) map[uint32]bool {
	kids := map[uint32][]uint32{}
	for _, p := range procs {
		kids[p.PPID] = append(kids[p.PPID], p.PID)
	}
	out := map[uint32]bool{root: true}
	queue := []uint32{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, k := range kids[pid] {
			if !out[k] && k != pid {
				out[k] = true
				queue = append(queue, k)
			}
		}
	}
	return out
}

// pickCPUTemp chooses the most meaningful processor temperature from sensor-app readings.
func pickCPUTemp(rs []sensorReading) (pkg float64, cores []Reading, ok bool) {
	best, bestRank := -1.0, 99
	for _, r := range rs {
		if r.Type != "Temperature" || !strings.Contains(strings.ToLower(r.Parent), "cpu") || r.Value <= 0 || r.Value > 125 {
			continue
		}
		n := strings.ToLower(r.Name)
		rank := 9
		switch {
		case n == "cpu package" || n == "package":
			rank = 0
		case strings.Contains(n, "tctl/tdie") || n == "core (tctl/tdie)" || n == "cpu (tctl/tdie)":
			rank = 1
		case strings.Contains(n, "tdie"):
			rank = 2
		case n == "core max" || n == "core average":
			rank = 3
		case strings.HasPrefix(n, "core #") || strings.HasPrefix(n, "cpu core #"):
			cores = append(cores, Reading{Name: r.Name, Value: r.Value})
			rank = 5
		}
		if rank < bestRank || (rank == bestRank && r.Value > best) {
			best, bestRank = r.Value, rank
		}
	}
	sort.SliceStable(cores, func(i, j int) bool { return naturalLess(cores[i].Name, cores[j].Name) })
	return best, cores, best > 0
}

func naturalLess(a, b string) bool {
	na, nb := trailingNum(a), trailingNum(b)
	if na >= 0 && nb >= 0 && strings.TrimRight(a, "0123456789") == strings.TrimRight(b, "0123456789") {
		return na < nb
	}
	return a < b
}

func trailingNum(s string) int {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return -1
	}
	n := 0
	for _, c := range s[i:] {
		n = n*10 + int(c-'0')
	}
	return n
}

// sensorReading is one value from LibreHardwareMonitor or OpenHardwareMonitor.
type sensorReading struct {
	Name   string  `json:"n"`
	Type   string  `json:"t"`
	Parent string  `json:"p"`
	Value  float64 `json:"v"`
}

// validZones drops thermal zones that report placeholder values.
func validZones(z []Reading) []Reading {
	var out []Reading
	for _, r := range z {
		if r.Value >= 15 && r.Value <= 120 && r.Value != 27.8 && r.Value != 28.0 {
			out = append(out, r)
		}
	}
	return out
}

// ---- the sampling loop ----

type monitor struct {
	mu      sync.Mutex
	latest  *Live
	started bool
	lastAsk time.Time
}

var mon = &monitor{}

func (m *monitor) get() *Live {
	m.mu.Lock()
	m.lastAsk = time.Now()
	if !m.started {
		m.started = true
		go m.loop()
	}
	l := m.latest
	m.mu.Unlock()
	if l == nil {
		// first request: take a quick sample so the window has something to show
		s := sampleLive()
		m.mu.Lock()
		if m.latest == nil {
			m.latest = s
		}
		l = m.latest
		m.mu.Unlock()
	}
	return l
}

func (m *monitor) loop() {
	startSensors()
	for {
		time.Sleep(time.Second)
		m.mu.Lock()
		idle := time.Since(m.lastAsk) > 15*time.Second
		m.mu.Unlock()
		if idle { // nobody is watching: sample slowly to stay light
			time.Sleep(4 * time.Second)
		}
		s := sampleLive()
		m.mu.Lock()
		m.latest = s
		m.mu.Unlock()
	}
}
