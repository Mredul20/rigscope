package main

import (
	"regexp"
	"strconv"
	"strings"
)

// Performance estimates.
//
// Rigscope doesn't benchmark. It estimates a processor's speed from its model
// name (which tells the generation and power class), core count and thread
// count, and looks up graphics chips in the table in catalog/catalog.json.
// Game and app requirements are scored the same way, so comparisons stay fair.
//
// CPU score, roughly: effective cores x generation factor x power class.
//   Intel Core i5-8250U (4C/8T laptop)  ~ 2.9
//   Intel Core i5-10400 (6C/12T)        ~ 5.9
//   AMD Ryzen 5 5600X (6C/12T)          ~ 7.8
//   Intel Core i7-13700K (16C/24T)      ~ 16
// GPU score: GeForce GTX 1060 = 100.

var cpuTierLimits = []float64{1.6, 2.6, 4.2, 7.0, 10.0}
var gpuTierLimits = []float64{12, 30, 60, 120, 250}

func tierOf(score float64, limits []float64) int {
	t := 1
	for _, l := range limits {
		if score >= l {
			t++
		}
	}
	return t
}

func gpuTier(score float64) int { return tierOf(score, gpuTierLimits) }

type CPUEstimate struct {
	Score  float64
	Tier   int
	Family string
	Win11  string // yes, no or unknown: is this processor on Microsoft's Windows 11 list?
	ARM    bool
	Known  bool
}

var (
	reClean     = regexp.MustCompile(`\((r|tm|c)\)|®|™`)
	reCoreUltra = regexp.MustCompile(`core ultra [3579] (\d)(\d{2})([a-z]*)`)
	reCoreI     = regexp.MustCompile(`\bi([3579])[- ](\d{3,5})([a-z]{0,3})`)
	reIntelN    = regexp.MustCompile(`(?:\bi3[- ]|processor |intel )n(\d{2,3})\b`)
	reCeleronN  = regexp.MustCompile(`(celeron|pentium)(?: silver| gold)? ?([njg])?(\d{4})`)
	reRyzen     = regexp.MustCompile(`ryzen [3579] (?:pro )?(\d)(\d)(\d)(\d)([a-z0-9]*)`)
	reRyzenAI   = regexp.MustCompile(`ryzen ai (?:max\+? |\d+ )?(?:pro )?(?:hx )?(\d)(\d{2})`)
	reThread    = regexp.MustCompile(`threadripper`)
	reEPYC      = regexp.MustCompile(`epyc ([0-9a-z]{3}\d)\b`)
)

func cleanName(s string) string {
	s = reClean.ReplaceAllString(strings.ToLower(s), " ")
	s = strings.NewReplacer(" cpu ", " ", "-core", " core", "processor", " processor ").Replace(" " + s + " ")
	return strings.Join(strings.Fields(s), " ")
}

func intelGenFactor(gen int) float64 {
	switch {
	case gen <= 3:
		return 0.5
	case gen <= 5:
		return 0.6
	case gen <= 7:
		return 0.7
	case gen <= 10:
		return 0.75
	case gen == 11:
		return 0.88
	case gen == 12:
		return 1.0
	}
	return 1.05
}

func intelClass(sfx string) float64 {
	switch {
	case sfx == "":
		return 1.0
	case strings.HasPrefix(sfx, "h"):
		return 0.95
	case strings.HasPrefix(sfx, "p"):
		return 0.88
	case strings.HasPrefix(sfx, "t"):
		return 0.85
	case strings.HasPrefix(sfx, "u"), strings.HasPrefix(sfx, "y"), strings.HasPrefix(sfx, "g"), strings.HasPrefix(sfx, "m"):
		return 0.75
	}
	return 1.0 // k, f, kf, ks, x
}

func amdClass(sfx string) float64 {
	switch {
	case strings.HasPrefix(sfx, "u"):
		return 0.78
	case strings.HasPrefix(sfx, "h"):
		return 0.95
	case strings.HasPrefix(sfx, "g"):
		return 0.92
	case strings.Contains(sfx, "x3d"):
		return 1.1
	}
	return 1.0
}

// effectiveCores weighs hyper-threads (and Intel's efficiency cores) below full cores.
func effectiveCores(cores, threads int, hybrid bool) float64 {
	if cores <= 0 {
		cores = 2
	}
	if threads < cores {
		threads = cores
	}
	if hybrid && threads > cores {
		// 12th-14th gen: performance cores have two threads, efficiency cores one.
		p := threads - cores
		e := cores - p
		if e < 0 {
			p, e = cores, 0
		}
		return float64(p)*1.3 + float64(e)*0.6
	}
	return float64(cores) + 0.3*float64(threads-cores)
}

func estimateCPU(name string, cores, threads int) CPUEstimate {
	n := cleanName(name)
	gen, cls, family, win11, hybrid, known, arm := 0.75, 1.0, "", "unknown", false, true, false

	if m := reCoreUltra.FindStringSubmatch(n); m != nil {
		series, _ := strconv.Atoi(m[1])
		gen, hybrid = 1.05, true
		if series >= 2 {
			gen = 1.15
		}
		cls = intelClass(m[3])
		if strings.HasPrefix(m[3], "v") {
			cls = 0.8
		}
		family, win11 = "Intel Core Ultra (Series "+m[1]+")", "yes"
	} else if m := reIntelN.FindStringSubmatch(n); m != nil && !strings.Contains(n, "celeron n4") && !strings.Contains(n, "celeron n5") {
		gen, cls, family, win11 = 0.75, 0.7, "Intel N-series", "yes"
	} else if m := reCoreI.FindStringSubmatch(n); m != nil {
		num := m[2]
		g := 1
		switch {
		case len(num) == 5:
			g, _ = strconv.Atoi(num[:2])
		case len(num) == 4 && num[0] == '1':
			g, _ = strconv.Atoi(num[:2])
		case len(num) == 4:
			g, _ = strconv.Atoi(num[:1])
		}
		gen, cls, hybrid = intelGenFactor(g), intelClass(m[3]), g >= 12
		family = "Intel Core " + ordinal(g) + " gen"
		if g >= 8 {
			win11 = "yes"
		} else {
			win11 = "no"
		}
	} else if m := reCeleronN.FindStringSubmatch(n); m != nil {
		gen, cls, family = 0.5, 0.75, "Intel "+strings.ToUpper(m[1][:1])+m[1][1:]
		if m[2] == "g" { // desktop Pentium Gold and Celeron G chips
			cls = 1.0
		}
	} else if strings.Contains(n, "celeron") || strings.Contains(n, "pentium") || strings.Contains(n, "atom") {
		gen, cls, family = 0.5, 0.8, "Intel Celeron, Pentium or Atom"
	} else if strings.Contains(n, "core 2") || strings.Contains(n, "core(2)") || strings.Contains(n, "core duo") {
		gen, family, win11 = 0.4, "Intel Core 2", "no"
	} else if strings.Contains(n, "xeon") {
		gen, family = 0.7, "Intel Xeon"
	} else if m := reRyzenAI.FindStringSubmatch(n); m != nil {
		gen, cls, family, win11 = 1.2, 0.9, "AMD Ryzen AI "+m[1]+"00 series", "yes"
		if strings.Contains(n, "max") {
			gen, cls = 1.25, 0.95
		}
	} else if m := reRyzen.FindStringSubmatch(n); m != nil {
		series, _ := strconv.Atoi(m[1])
		third := m[3]
		sfx := m[5]
		mobile := strings.HasPrefix(sfx, "u") || strings.HasPrefix(sfx, "h")
		switch series {
		case 1:
			gen = 0.65
		case 2:
			gen = 0.78
		case 3:
			gen = 0.88
			if mobile {
				gen = 0.8
			}
		case 4:
			gen = 0.9
		case 5:
			gen = 0.98
		case 6:
			gen = 1.05
		case 7:
			switch third {
			case "2":
				gen = 0.9
			case "3":
				gen = 1.0
			default:
				gen = 1.15
			}
		case 8:
			gen = 1.15
		default:
			gen = 1.25
		}
		cls = amdClass(sfx)
		family = "AMD Ryzen " + m[1] + "000 series"
		if series >= 2 {
			win11 = "yes"
		} else {
			win11 = "no"
		}
	} else if m := reEPYC.FindStringSubmatch(n); m != nil {
		// EPYC model numbers end in the generation: 7xx1 Zen, 7xx2 Zen 2, 7xx3 Zen 3, 9xx4 Zen 4, 9xx5 Zen 5
		switch m[1][len(m[1])-1] {
		case '1':
			gen = 0.65
		case '2':
			gen = 0.85
		case '3':
			gen = 0.95
		case '4':
			gen = 1.1
		default:
			gen = 1.2
		}
		family, win11 = "AMD EPYC (server)", "unknown"
	} else if reThread.MatchString(n) {
		gen, family, win11 = 1.0, "AMD Ryzen Threadripper", "yes"
	} else if strings.Contains(n, "athlon") {
		gen, cls, family = 0.6, 0.85, "AMD Athlon"
		if strings.Contains(n, "athlon ii") || strings.Contains(n, "athlon 64") {
			gen, win11 = 0.35, "no"
		}
	} else if strings.Contains(n, " fx") || strings.HasPrefix(n, "fx") || regexp.MustCompile(`\ba(4|6|8|9|10|12)[- ]\d{4}`).MatchString(n) || regexp.MustCompile(`\be[12]-\d{4}`).MatchString(n) {
		gen, family, win11 = 0.45, "AMD FX or A-series", "no"
	} else if strings.Contains(n, "snapdragon") || strings.Contains(n, "qualcomm") || strings.Contains(n, "microsoft sq") {
		gen, cls, family, win11, arm = 1.0, 0.85, "Qualcomm Snapdragon (ARM)", "yes", true
		if strings.Contains(n, "sq1") || strings.Contains(n, "sq2") || strings.Contains(n, "8cx") || strings.Contains(n, "7c") {
			gen = 0.65
		}
	} else {
		known = false
		family = "Unrecognized processor"
	}

	score := effectiveCores(cores, threads, hybrid) * gen * cls
	return CPUEstimate{Score: score, Tier: tierOf(score, cpuTierLimits), Family: family, Win11: win11, ARM: arm, Known: known}
}

func ordinal(n int) string {
	s := strconv.Itoa(n)
	if n%100 >= 11 && n%100 <= 13 {
		return s + "th"
	}
	switch n % 10 {
	case 1:
		return s + "st"
	case 2:
		return s + "nd"
	case 3:
		return s + "rd"
	}
	return s + "th"
}

// ---- graphics ----

type GPUInfo struct {
	Score      float64
	Integrated bool
	Found      bool
	Match      string
}

func isAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

// containsWord reports whether needle appears in hay with no letter or digit touching either end.
func containsWord(hay, needle string) bool {
	for i := 0; ; {
		j := strings.Index(hay[i:], needle)
		if j < 0 {
			return false
		}
		s, e := i+j, i+j+len(needle)
		if (s == 0 || !isAlnum(hay[s-1])) && (e == len(hay) || !isAlnum(hay[e])) {
			return true
		}
		i = s + 1
	}
}

func lookupGPU(name string) GPUInfo {
	n := cleanName(name)
	n = strings.NewReplacer("nvidia ", "", "geforce ", "", "amd ", "", "intel ", "", "radeon rx ", "rx ", "(r)", "").Replace(" " + n + " ")
	n = strings.Join(strings.Fields(n), " ")
	laptop := strings.Contains(n, "laptop") || strings.Contains(n, "max-q") || strings.Contains(n, "mobile") || reMobileGPU.MatchString(n)
	for _, g := range catalog.gpuIndex { // sorted longest match first
		if containsWord(n, g.Match) {
			s := g.Score
			if laptop && !g.Integrated && !g.Laptop {
				s *= 0.82
			}
			return GPUInfo{Score: s, Integrated: g.Integrated, Found: true, Match: g.Match}
		}
	}
	return GPUInfo{}
}
