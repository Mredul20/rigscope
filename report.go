package main

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---- raw data, exactly as ps/specs.ps1 prints it ----

type rawSystem struct {
	ComputerName string `json:"computerName"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Family       string `json:"family"`
	PCType       int    `json:"pcType"`
	Chassis      []int  `json:"chassis"`
	Board        string `json:"board"`
	BIOS         string `json:"bios"`
	BIOSDate     string `json:"biosDate"`
	Firmware     string `json:"firmware"`
	Hypervisor   bool   `json:"hypervisor"`
	TotalMemory  int64  `json:"totalMemory"`
}

type rawOS struct {
	Caption        string `json:"caption"`
	Version        string `json:"version"`
	Build          int    `json:"build"`
	UBR            int    `json:"ubr"`
	DisplayVersion string `json:"displayVersion"`
	Edition        string `json:"edition"`
	Arch           string `json:"arch"`
	InstallDate    string `json:"installDate"`
	LastBoot       string `json:"lastBoot"`
	FreeMemoryKB   int64  `json:"freeMemoryKB"`
}

type rawCPU struct {
	Name         string `json:"name"`
	Maker        string `json:"maker"`
	Sockets      int    `json:"sockets"`
	Cores        int    `json:"cores"`
	Threads      int    `json:"threads"`
	BaseMHz      int    `json:"baseMHz"`
	CurrentMHz   int    `json:"currentMHz"`
	L1KB         int64  `json:"l1KB"`
	L2KB         int64  `json:"l2KB"`
	L3KB         int64  `json:"l3KB"`
	Arch         int    `json:"arch"`
	Socket       string `json:"socket"`
	VirtFirmware bool   `json:"virtFirmware"`
}

type rawModule struct {
	Capacity   int64  `json:"capacity"`
	Speed      int    `json:"speed"`
	Configured int    `json:"configured"`
	Maker      string `json:"maker"`
	Part       string `json:"part"`
	Slot       string `json:"slot"`
	Type       int    `json:"type"`
	FormFactor int    `json:"formFactor"`
}

type rawGPU struct {
	Name       string `json:"name"`
	Memory     int64  `json:"memory"`
	Driver     string `json:"driver"`
	DriverDate string `json:"driverDate"`
	PNP        string `json:"pnp"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Refresh    int    `json:"refresh"`
	Status     string `json:"status"`
}

type Volume struct {
	Letter string `json:"letter"`
	Label  string `json:"label"`
	FS     string `json:"fs"`
	Size   int64  `json:"size"`
	Free   int64  `json:"free"`
}

type rawAV struct {
	Name  string `json:"name"`
	State int64  `json:"state"`
}

type rawSecurity struct {
	AV       []rawAV `json:"av"`
	FW3P     []rawAV `json:"fw3p"`
	Defender *struct {
		Enabled  bool   `json:"enabled"`
		Realtime bool   `json:"realtime"`
		SigAge   int    `json:"sigAge"`
		Tamper   bool   `json:"tamper"`
		Service  bool   `json:"service"`
		Mode     string `json:"mode"`
	} `json:"defender"`
	Firewall []struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	} `json:"firewall"`
	SecureBoot int64  `json:"secureBoot"`
	TPMName    string `json:"tpmName"`
	TPMOK      bool   `json:"tpmOK"`
	TPMSpec    string `json:"tpmSpec"`
	TPMReady   *bool  `json:"tpmReady"`
	BitLocker  []struct {
		Letter string `json:"letter"`
		State  int64  `json:"state"`
	} `json:"bitlocker"`
	LastUpdate        string `json:"lastUpdate"`
	LastUpdateID      string `json:"lastUpdateId"`
	UAC               int64  `json:"uac"`
	UACPrompt         int64  `json:"uacPrompt"`
	MemoryIntegrity   int64  `json:"memoryIntegrity"`
	SmartScreen       string `json:"smartScreen"`
	SmartScreenPolicy int64  `json:"smartScreenPolicy"`
}

type rawSpecs struct {
	System rawSystem `json:"system"`
	OS     rawOS     `json:"os"`
	CPU    rawCPU    `json:"cpu"`
	Memory struct {
		Modules []rawModule `json:"modules"`
		Slots   int         `json:"slots"`
	} `json:"memory"`
	GPUs     []rawGPU    `json:"gpus"`
	Volumes  []Volume    `json:"volumes"`
	Disks    []Disk      `json:"disks"`
	Security rawSecurity `json:"security"`
	Errors   []string    `json:"errors"`
}

func parseSpecs(b []byte) (*rawSpecs, error) {
	b = []byte(strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff")))
	if len(b) == 0 {
		return nil, errors.New("Windows returned no hardware information")
	}
	var r rawSpecs
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	for i := range r.Disks {
		finishDisk(&r.Disks[i])
	}
	return &r, nil
}

// ---- the report the window shows ----

type Report struct {
	Generated string      `json:"generated"`
	Version   string      `json:"version"`
	Admin     bool        `json:"admin"`
	System    rawSystem   `json:"system"`
	Laptop    bool        `json:"laptop"`
	OS        OSInfo      `json:"os"`
	CPU       CPUReport   `json:"cpu"`
	Memory    MemReport   `json:"memory"`
	GPUs      []GPUReport `json:"gpus"`
	Disks     []Disk      `json:"disks"`
	Volumes   []Volume    `json:"volumes"`
	Battery   BatteryInfo `json:"battery"`
	Run       RunReport   `json:"run"`
	Security  SecReport   `json:"security"`
	Errors    []string    `json:"errors"`
}

type OSInfo struct {
	rawOS
	Name     string `json:"name"`
	Friendly string `json:"friendly"`
}

type CPUReport struct {
	rawCPU
	Score    float64 `json:"score"`
	Tier     int     `json:"tier"`
	Family   string  `json:"family"`
	Win11    string  `json:"win11"`
	ArchName string  `json:"archName"`
	ARM      bool    `json:"arm"`
	Virt     bool    `json:"virt"`
}

type MemModule struct {
	rawModule
	TypeName string `json:"typeName"`
}

type MemReport struct {
	Total    int64       `json:"total"`
	Usable   int64       `json:"usable"`
	Free     int64       `json:"free"`
	Modules  []MemModule `json:"modules"`
	Slots    int         `json:"slots"`
	TypeName string      `json:"typeName"`
	Speed    int         `json:"speed"`
	Channels string      `json:"channels"`
}

type GPUReport struct {
	rawGPU
	Vendor     string  `json:"vendor"`
	Integrated bool    `json:"integrated"`
	Score      float64 `json:"score"`
	Tier       int     `json:"tier"`
	Estimated  bool    `json:"estimated"`
	NoDriver   bool    `json:"noDriver"`
}

var memTypes = map[int]string{20: "DDR", 21: "DDR2", 24: "DDR3", 26: "DDR4", 27: "LPDDR", 28: "LPDDR2", 29: "LPDDR3", 30: "LPDDR4", 34: "DDR5", 35: "LPDDR5"}

func buildReport(raw *rawSpecs, bat BatteryInfo, admin bool, now time.Time) *Report {
	r := &Report{Generated: now.Format(time.RFC3339), Version: version, Admin: admin, System: raw.System,
		Disks: raw.Disks, Volumes: raw.Volumes, Battery: bat, Errors: raw.Errors}
	if r.Disks == nil {
		r.Disks = []Disk{}
	}
	if r.Volumes == nil {
		r.Volumes = []Volume{}
	}
	if r.Errors == nil {
		r.Errors = []string{}
	}
	r.Laptop = isLaptop(raw.System) || bat.Live.HasBattery
	r.OS = osInfo(raw.OS)

	// processor
	c := CPUReport{rawCPU: raw.CPU}
	est := estimateCPU(raw.CPU.Name, raw.CPU.Cores, raw.CPU.Threads)
	c.Score, c.Tier, c.Family, c.Win11 = round1(est.Score), est.Tier, est.Family, est.Win11
	c.ARM = raw.CPU.Arch == 12 || est.ARM
	c.ArchName = map[int]string{0: "x86 (32-bit)", 5: "ARM", 9: "x64", 12: "ARM64"}[raw.CPU.Arch]
	c.Virt = raw.CPU.VirtFirmware || raw.System.Hypervisor
	r.CPU = c

	// memory
	m := MemReport{Usable: raw.System.TotalMemory, Free: raw.OS.FreeMemoryKB * 1024, Slots: raw.Memory.Slots, Modules: []MemModule{}}
	types := map[string]bool{}
	for _, mod := range raw.Memory.Modules {
		mm := MemModule{rawModule: mod, TypeName: memTypes[mod.Type]}
		m.Total += mod.Capacity
		if mm.TypeName != "" {
			types[mm.TypeName] = true
		}
		sp := mod.Configured
		if sp == 0 {
			sp = mod.Speed
		}
		if sp > 0 && (m.Speed == 0 || sp < m.Speed) {
			m.Speed = sp // the slowest module sets the speed for all of them
		}
		m.Modules = append(m.Modules, mm)
	}
	if m.Total == 0 {
		m.Total = m.Usable
	}
	for t := range types {
		m.TypeName = t
	}
	switch n := len(m.Modules); {
	case n == 1:
		m.Channels = "single"
	case n >= 2:
		m.Channels = "dual"
	}
	r.Memory = m

	// graphics
	r.GPUs = []GPUReport{}
	for _, g := range raw.GPUs {
		if isVirtualAdapter(g.Name) && len(raw.GPUs) > 1 {
			continue
		}
		gr := GPUReport{rawGPU: g, Vendor: gpuVendor(g)}
		info := lookupGPU(g.Name)
		gr.Integrated = info.Integrated
		gr.NoDriver = strings.Contains(strings.ToLower(g.Name), "basic display") || strings.Contains(strings.ToLower(g.Name), "basic render")
		if info.Found {
			gr.Score = info.Score
		} else {
			gr.Score, gr.Integrated = guessGPU(g), gr.Integrated || gr.Vendor == "Intel"
			gr.Estimated = true
		}
		if gr.NoDriver {
			gr.Score = 5
		}
		gr.Tier = gpuTier(gr.Score)
		r.GPUs = append(r.GPUs, gr)
	}
	sort.SliceStable(r.GPUs, func(i, j int) bool { return r.GPUs[i].Score > r.GPUs[j].Score })

	r.Security = evaluateSecurity(raw, r, now)
	r.Run = rateAll(r)
	return r
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func isLaptop(s rawSystem) bool {
	if s.PCType == 2 {
		return true
	}
	for _, c := range s.Chassis {
		switch c {
		case 8, 9, 10, 11, 14, 30, 31, 32: // portable, laptop, notebook, handheld, sub notebook, tablet, convertible, detachable
			return true
		}
	}
	return false
}

func osInfo(o rawOS) OSInfo {
	info := OSInfo{rawOS: o}
	name := strings.TrimSpace(strings.TrimPrefix(o.Caption, "Microsoft "))
	if name == "" {
		name = "Windows"
	}
	// Windows 11 still reports version 10.0, so the build number is what tells them apart.
	if o.Build >= 22000 && strings.Contains(name, "Windows 10") {
		name = strings.Replace(name, "Windows 10", "Windows 11", 1)
	}
	info.Name = name
	f := name
	if o.DisplayVersion != "" {
		f += " " + o.DisplayVersion
	}
	if o.Build > 0 {
		f += " (build " + itoa(o.Build)
		if o.UBR > 0 {
			f += "." + itoa(o.UBR)
		}
		f += ")"
	}
	info.Friendly = f
	return info
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func isVirtualAdapter(name string) bool {
	n := strings.ToLower(name)
	for _, v := range []string{"remote display", "parsec", "virtual display", "idd", "spacedesk", "meta virtual", "citrix", "dameware", "oray", "usb mobile monitor", "displaylink"} {
		if strings.Contains(n, v) {
			return true
		}
	}
	return false
}

func gpuVendor(g rawGPU) string {
	p := strings.ToUpper(g.PNP)
	n := strings.ToLower(g.Name)
	switch {
	case strings.Contains(p, "VEN_10DE") || strings.Contains(n, "nvidia") || strings.Contains(n, "geforce"):
		return "NVIDIA"
	case strings.Contains(p, "VEN_1002") || strings.Contains(n, "amd") || strings.Contains(n, "radeon"):
		return "AMD"
	case strings.Contains(p, "VEN_8086") || strings.Contains(n, "intel"):
		return "Intel"
	case strings.Contains(p, "QCOM") || strings.Contains(n, "adreno") || strings.Contains(n, "qualcomm"):
		return "Qualcomm"
	}
	return ""
}

// guessGPU gives a rough score for graphics chips missing from the catalog, based on video memory.
func guessGPU(g rawGPU) float64 {
	gb := float64(g.Memory) / (1 << 30)
	switch v := gpuVendor(g); {
	case v == "Intel" || v == "Qualcomm":
		return 15
	case gb >= 12:
		return 300
	case gb >= 8:
		return 180
	case gb >= 6:
		return 120
	case gb >= 4:
		return 70
	case gb >= 2:
		return 35
	}
	return 12
}

// ---- caching: collecting takes a few seconds, so keep one report until asked to refresh ----

var (
	reportSnap atomic.Pointer[Report] // latest report, readable without waiting for a refresh
	reportMu   sync.Mutex
	lastReport *Report
	lastErr    error
)

func getReport(refresh bool) (*Report, error) {
	reportMu.Lock()
	defer reportMu.Unlock()
	if lastReport != nil && !refresh {
		return lastReport, nil
	}
	var (
		raw  *rawSpecs
		rerr error
		bat  BatteryInfo
		wg   sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		out, err := runPowerShell(specsScript)
		if err != nil {
			rerr = err
			return
		}
		raw, rerr = parseSpecs(out)
	}()
	go func() { defer wg.Done(); bat = readBattery() }()
	wg.Wait()
	if rerr != nil {
		lastErr = rerr
		return nil, rerr
	}
	lastReport, lastErr = buildReport(raw, bat, isAdmin(), time.Now()), nil
	reportSnap.Store(lastReport)
	return lastReport, nil
}
