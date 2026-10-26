package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

//go:embed catalog/catalog.json
var catalogJSON []byte

type RefCPU struct {
	Name    string `json:"name"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
}

type Spec struct {
	CPU RefCPU `json:"cpu"`
	GPU string `json:"gpu"`
	RAM int    `json:"ram"` // GB
}

type Item struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // game or app
	Category string `json:"category"`
	Min      Spec   `json:"min"`
	Rec      *Spec  `json:"rec,omitempty"`
	Storage  int    `json:"storage"` // GB
	SSD      bool   `json:"ssd"`
	// Anti-cheat that refuses to start without TPM 2.0 and Secure Boot.
	NeedsTPMSecureBoot      bool   `json:"needsTpmSecureBoot"`
	NeedsTPMSecureBootWin11 bool   `json:"needsTpmSecureBootWin11"`
	NeedsVirtualization     bool   `json:"needsVirtualization"`
	NoARM                   bool   `json:"noArm"`
	Note                    string `json:"note"`
}

type GPUEntry struct {
	Match      string  `json:"m"`
	Score      float64 `json:"s"`
	Integrated bool    `json:"i,omitempty"`
	Laptop     bool    `json:"-"`
}

type WinVersion struct {
	Build     int    `json:"build"`
	Name      string `json:"name"`
	HomeEnd   string `json:"homeEnd"`
	EntEnd    string `json:"entEnd"`
	LTSCEnd   string `json:"ltscEnd,omitempty"`
	LatestUBR int    `json:"latestUbr,omitempty"`
}

type Catalog struct {
	Updated string     `json:"updated"`
	Items   []Item     `json:"items"`
	GPUs    []GPUEntry `json:"gpus"`
	Windows struct {
		AsOf     string       `json:"asOf"`
		Win11    []WinVersion `json:"win11"`
		Win10    WinVersion   `json:"win10"`
		Win10ESU string       `json:"win10EsuEnd"`
		Win10LTS []WinVersion `json:"win10Ltsc"`
	} `json:"windows"`

	gpuIndex []GPUEntry
}

var reMobileGPU = regexp.MustCompile(`\d{3,4}m\b|\d{3}mx\b|\bmx\d{3}\b`)

var catalog = mustLoadCatalog(catalogJSON)

func mustLoadCatalog(b []byte) *Catalog {
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		panic("catalog/catalog.json is not valid JSON: " + err.Error())
	}
	c.gpuIndex = append([]GPUEntry(nil), c.GPUs...)
	for i := range c.gpuIndex {
		c.gpuIndex[i].Match = strings.ToLower(c.gpuIndex[i].Match)
		m := c.gpuIndex[i].Match
		c.gpuIndex[i].Laptop = strings.Contains(m, "laptop") || reMobileGPU.MatchString(m)
	}
	sort.SliceStable(c.gpuIndex, func(i, j int) bool { return len(c.gpuIndex[i].Match) > len(c.gpuIndex[j].Match) })
	return &c
}

// ---- rating ----

type Rating struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Category string   `json:"category"`
	Status   string   `json:"status"` // great, ok, low, blocked
	Label    string   `json:"label"`
	Reasons  []string `json:"reasons"`
	Needs    string   `json:"needs"`
	Note     string   `json:"note"`
	Storage  int      `json:"storage"`
}

type RunReport struct {
	CPUScore  float64  `json:"cpuScore"`
	CPUTier   int      `json:"cpuTier"`
	GPUScore  float64  `json:"gpuScore"`
	GPUTier   int      `json:"gpuTier"`
	GPUName   string   `json:"gpuName"`
	RAMGB     int      `json:"ramGB"`
	Summary   string   `json:"summary"`
	Items     []Rating `json:"items"`
	Updated   string   `json:"updated"`
	Estimated bool     `json:"estimated"`
}

const tolerance = 0.9 // within 10% counts as meeting a requirement: these are estimates, not benchmarks

// When a publisher lists only minimum specs, having this many times the minimum counts as "runs well".
const noRecMargin = 2.0

func refCPUScore(c RefCPU) float64 { return estimateCPU(c.Name, c.Cores, c.Threads).Score }
func refGPUScore(name string) float64 {
	if g := lookupGPU(name); g.Found {
		return g.Score
	}
	return 0
}

func ramGB(bytes int64) int { return int(math.Round(float64(bytes) / (1 << 30))) }

func rateAll(r *Report) RunReport {
	run := RunReport{CPUScore: r.CPU.Score, CPUTier: r.CPU.Tier, RAMGB: ramGB(r.Memory.Total), Items: []Rating{}, Updated: catalog.Updated}
	if len(r.GPUs) > 0 {
		run.GPUScore, run.GPUTier, run.GPUName, run.Estimated = round1(r.GPUs[0].Score), r.GPUs[0].Tier, r.GPUs[0].Name, r.GPUs[0].Estimated
	}
	var maxFree int64
	hasSSD := false
	for _, v := range r.Volumes {
		if v.Free > maxFree {
			maxFree = v.Free
		}
	}
	for _, d := range r.Disks {
		if d.Kind == "SSD" {
			hasSSD = true
		}
	}
	win11 := r.OS.Build >= 22000
	tpm := r.Security.raw.TPMOK && (strings.Contains(r.Security.raw.TPMName, "2.0") || strings.HasPrefix(r.Security.raw.TPMSpec, "2.0"))
	sb := r.Security.raw.SecureBoot == 1
	is64 := !strings.Contains(r.OS.Arch, "32")

	great, ok := 0, 0
	for _, it := range catalog.Items {
		rt := Rating{ID: it.ID, Name: it.Name, Kind: it.Kind, Category: it.Category, Note: it.Note, Storage: it.Storage, Reasons: []string{}}
		minCPU, minGPU := refCPUScore(it.Min.CPU), refGPUScore(it.Min.GPU)
		var below, short []string
		if r.CPU.Score < minCPU*tolerance {
			below = append(below, "Processor is slower than the minimum ("+it.Min.CPU.Name+")")
		}
		if run.GPUScore < minGPU*tolerance {
			below = append(below, "Graphics are weaker than the minimum ("+it.Min.GPU+")")
		}
		if run.RAMGB < it.Min.RAM {
			below = append(below, fmt.Sprintf("Needs at least %d GB of memory, this PC has %d GB", it.Min.RAM, run.RAMGB))
		}
		blocked := ""
		if !is64 {
			blocked = "Needs 64-bit Windows"
		}
		if it.NeedsTPMSecureBoot || (it.NeedsTPMSecureBootWin11 && win11) {
			switch {
			case !tpm && !sb:
				blocked = "Its anti-cheat needs TPM 2.0 and Secure Boot turned on"
			case !tpm:
				blocked = "Its anti-cheat needs TPM 2.0 turned on"
			case !sb:
				blocked = "Its anti-cheat needs Secure Boot turned on"
			}
		}
		if it.NeedsVirtualization && !r.CPU.Virt {
			blocked = "Needs virtualization turned on in the BIOS or UEFI settings"
		}
		if it.NoARM && r.CPU.ARM {
			blocked = "Doesn't support Windows on ARM processors"
		}

		if it.Rec != nil {
			if r.CPU.Score < refCPUScore(it.Rec.CPU)*tolerance {
				short = append(short, "processor")
			}
			if run.GPUScore < refGPUScore(it.Rec.GPU)*tolerance {
				short = append(short, "graphics")
			}
			if run.RAMGB < it.Rec.RAM {
				short = append(short, "memory")
			}
		}
		switch {
		case blocked != "":
			rt.Status, rt.Label = "blocked", "Won't start"
			rt.Reasons = append(rt.Reasons, blocked)
			rt.Reasons = append(rt.Reasons, below...)
		case len(below) > 0:
			rt.Status, rt.Label = "low", "Below minimum"
			rt.Reasons = below
		case it.Rec == nil && r.CPU.Score >= minCPU*noRecMargin && run.GPUScore >= minGPU*noRecMargin:
			// Only a minimum is published: comfortably beating it counts as running well.
			rt.Status, rt.Label = "great", "Runs well"
			great++
		case it.Rec == nil:
			rt.Status, rt.Label = "ok", "Meets the minimum"
			ok++
		case len(short) == 0:
			rt.Status, rt.Label = "great", "Runs well"
			great++
		default:
			rt.Status, rt.Label = "ok", "Runs on lower settings"
			if it.Kind == "app" {
				rt.Label = "Runs, but slowly with big projects"
			}
			rt.Reasons = append(rt.Reasons, "Below the recommended "+joinAnd(short))
			ok++
		}
		if it.SSD && !hasSSD && rt.Status != "blocked" {
			rt.Reasons = append(rt.Reasons, "Should be installed on an SSD, and this PC has none, so loading will be slow")
			if rt.Status == "great" {
				rt.Status, rt.Label = "ok", "Runs, but loads slowly"
				great--
				ok++
			}
		}
		if it.Storage > 0 && maxFree > 0 && maxFree < int64(it.Storage)<<30 {
			rt.Reasons = append(rt.Reasons, fmt.Sprintf("Needs %d GB of free space, the most free on any drive is %d GB", it.Storage, maxFree>>30))
		}
		if it.Kind == "game" && r.CPU.ARM && rt.Status != "blocked" {
			rt.Reasons = append(rt.Reasons, "Runs through x86 emulation on this ARM processor, so expect lower speed")
		}
		rt.Needs = describeNeeds(it)
		run.Items = append(run.Items, rt)
	}
	run.Summary = summarize(r, run)
	return run
}

func describeNeeds(it Item) string {
	s := fmt.Sprintf("Minimum: %s, %s, %d GB memory.", it.Min.CPU.Name, it.Min.GPU, it.Min.RAM)
	if it.Rec != nil {
		s += fmt.Sprintf(" Recommended: %s, %s, %d GB memory.", it.Rec.CPU.Name, it.Rec.GPU, it.Rec.RAM)
	}
	return s
}

func joinAnd(p []string) string {
	switch len(p) {
	case 0:
		return ""
	case 1:
		return p[0]
	case 2:
		return p[0] + " and " + p[1]
	}
	return strings.Join(p[:len(p)-1], ", ") + " and " + p[len(p)-1]
}

var tierWords = []string{"", "very basic", "basic", "entry-level", "mid-range", "strong", "high-end"}

func summarize(r *Report, run RunReport) string {
	games, gamesGreat, gamesOK := 0, 0, 0
	for _, it := range run.Items {
		if it.Kind == "game" {
			games++
			switch it.Status {
			case "great":
				gamesGreat++
			case "ok":
				gamesOK++
			}
		}
	}
	use := ""
	switch g := run.GPUTier; {
	case g <= 2 && run.CPUTier <= 2:
		use = "Best for browsing, office work, video calls and light games."
	case g <= 2:
		use = "Good for office work, coding and light games. The graphics hold it back in modern 3D games."
	case g == 3:
		use = "Good for everyday work and popular online games at lower settings."
	case g == 4:
		use = "Handles most games at 1080p on medium settings, plus photo and video editing."
	default:
		use = "Handles demanding games, video editing and 3D work."
	}
	if run.RAMGB > 0 && run.RAMGB < 8 {
		use += " More memory (8 GB or more) would help the most."
	}
	return fmt.Sprintf("A %s processor with %s graphics. %s It runs %d of %d games in the list well and %d more on lower settings.",
		tierWords[run.CPUTier], tierWords[clampTier(run.GPUTier)], use, gamesGreat, games, gamesOK)
}

func clampTier(t int) int {
	if t < 1 {
		return 1
	}
	if t > 6 {
		return 6
	}
	return t
}
