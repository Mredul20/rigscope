//go:build !windows

package main

// Simulated live data for developing the window on Linux and macOS. It moves
// like a real PC: a busy browser, a game or export in bursts, heat that lags
// behind load.

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

type fakeApp struct {
	key, exe, desc string
	kind           string
	count          int
	cpu, mem, io   float64 // typical values
	gpu            float64
	burst          float64 // how spiky
}

var (
	fakeMu    sync.Mutex
	fakeStart = time.Now()
	fakeHeat  = 48.0
	fakeLoad  = make([]float64, 16)
	fakeRnd   = rand.New(rand.NewSource(7))
)

func startSensors() {}

func laptopSample() bool { return sampleName() != "desktop" }

func fakeApps() []fakeApp {
	if laptopSample() {
		return []fakeApp{
			{"chrome.exe", "chrome.exe", "Google Chrome", "app", 23, 9, 1.6e9, 2.4e5, 4, 1.5},
			{"code.exe", "Code.exe", "Visual Studio Code", "app", 11, 3.5, 7.4e8, 6e4, 0.5, 1.2},
			{"msmpeng.exe", "MsMpEng.exe", "Antimalware Service Executable", "background", 1, 4, 2.1e8, 1.8e6, 0, 2.5},
			{"msedgewebview2.exe", "msedgewebview2.exe", "Microsoft Edge WebView2", "background", 9, 1.2, 4.1e8, 1e4, 0.4, 1},
			{"explorer.exe", "explorer.exe", "Windows Explorer", "app", 1, 0.8, 1.2e8, 3e3, 0, 1},
			{"dwm.exe", "dwm.exe", "Desktop Window Manager", "windows", 1, 2.2, 1.1e8, 0, 6, 0.8},
			{"system", "System", "", "windows", 1, 1.4, 1.5e5, 4e5, 0, 1.5},
			{"memory compression", "Memory Compression", "", "windows", 1, 0.2, 6.2e8, 0, 0, 0.5},
			{"svchost.exe", "svchost.exe", "Host Process for Windows Services", "windows", 74, 1.6, 3.9e8, 9e4, 0, 1},
			{"onedrive.exe", "OneDrive.exe", "Microsoft OneDrive", "background", 1, 0.6, 9e7, 1.2e5, 0, 2},
			{"discord.exe", "Discord.exe", "Discord", "app", 6, 1.1, 3.6e8, 2e4, 0.6, 1},
			{"spotify.exe", "Spotify.exe", "Spotify", "app", 5, 0.9, 2.4e8, 1.4e4, 0.2, 0.8},
			{"searchhost.exe", "SearchHost.exe", "Search", "background", 1, 0.1, 1.1e8, 0, 0, 0.5},
			{"phoneexperiencehost.exe", "PhoneExperienceHost.exe", "Microsoft Phone Link", "background", 1, 0.1, 6e7, 0, 0, 0.5},
			{"audiodg.exe", "audiodg.exe", "Windows Audio Device Graph Isolation", "windows", 1, 0.4, 1.4e7, 0, 0, 0.5},
			{"#rigscope", "Rigscope.exe", "", "self", 7, 0.7, 1.3e8, 2e3, 0.3, 0.6},
			{"ctfmon.exe", "ctfmon.exe", "CTF Loader", "windows", 1, 0, 1.2e7, 0, 0, 0},
			{"widgets.exe", "Widgets.exe", "Widgets", "background", 1, 0, 3.2e7, 0, 0, 0},
		}
	}
	return []fakeApp{
		{"cyberpunk2077.exe", "Cyberpunk2077.exe", "Cyberpunk 2077", "app", 1, 31, 8.9e9, 6.4e6, 94, 0.4},
		{"chrome.exe", "chrome.exe", "Google Chrome", "app", 18, 2.4, 1.9e9, 4e4, 1, 1},
		{"discord.exe", "Discord.exe", "Discord", "app", 7, 1.8, 4.6e8, 3e4, 2, 1},
		{"obs64.exe", "obs64.exe", "OBS Studio", "app", 1, 4.2, 6.1e8, 8.4e6, 12, 0.5},
		{"steam.exe", "steam.exe", "Steam", "background", 1, 0.4, 2.1e8, 1e4, 0, 1},
		{"steamwebhelper.exe", "steamwebhelper.exe", "Steam Client WebHelper", "background", 6, 0.6, 3.3e8, 1e4, 0.5, 1},
		{"dwm.exe", "dwm.exe", "Desktop Window Manager", "windows", 1, 1.2, 1.8e8, 0, 3, 1},
		{"system", "System", "", "windows", 1, 0.8, 1.5e5, 2e5, 0, 1},
		{"svchost.exe", "svchost.exe", "Host Process for Windows Services", "windows", 81, 1.1, 4.4e8, 7e4, 0, 1},
		{"nvcontainer.exe", "nvcontainer.exe", "NVIDIA Container", "background", 3, 0.3, 9e7, 0, 0, 1},
		{"msmpeng.exe", "MsMpEng.exe", "Antimalware Service Executable", "background", 1, 0.5, 2.3e8, 1e5, 0, 2},
		{"explorer.exe", "explorer.exe", "Windows Explorer", "app", 1, 0.4, 1.4e8, 2e3, 0, 1},
		{"memory compression", "Memory Compression", "", "windows", 1, 0, 9e7, 0, 0, 0},
		{"#rigscope", "Rigscope.exe", "", "self", 7, 0.4, 1.3e8, 2e3, 0.3, 0.6},
		{"spotify.exe", "Spotify.exe", "Spotify", "app", 5, 0.5, 2.6e8, 1e4, 0.2, 0.8},
	}
}

func wobble(base, spread float64) float64 {
	return math.Max(0, base*(1+spread*(fakeRnd.Float64()*2-1)))
}

func sampleLive() *Live {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	t := time.Since(fakeStart).Seconds()
	laptop := laptopSample()
	ncores := 4
	if !laptop {
		ncores = 12
	}
	// a slow wave of activity with occasional spikes
	wave := 0.5 + 0.5*math.Sin(t/9)
	spike := 0.0
	if fakeRnd.Float64() < 0.12 {
		spike = fakeRnd.Float64() * 45
	}
	cores := make([]float64, ncores)
	var total float64
	for i := range cores {
		target := 10 + 35*wave + spike*fakeRnd.Float64()
		if !laptop {
			target = 38 + 30*wave + spike*fakeRnd.Float64()
		}
		if i%3 == 0 {
			target += 12
		}
		fakeLoad[i] += (target - fakeLoad[i]) * 0.45
		cores[i] = round1(clamp(fakeLoad[i]+fakeRnd.Float64()*8-4, 1, 100))
		total += cores[i]
	}
	total /= float64(ncores)
	// heat follows load slowly
	targetHeat := 46 + total*0.42
	if !laptop {
		targetHeat = 50 + total*0.33
	}
	fakeHeat += (targetHeat - fakeHeat) * 0.18
	base := 2712.0
	if !laptop {
		base = 3701
	}
	mhz := base * (1.02 + 0.12*wave + 0.0012*total)

	l := &Live{Time: time.Now().UnixMilli(), Sources: []string{}, Warnings: []string{}}
	l.CPU = LiveCPU{Total: round1(total), Cores: cores, MHz: round1(mhz), BaseMHz: base, Limit: 100, Power: -1}
	l.Temp = LiveTemp{CPU: round1(fakeHeat), Source: "thermalzone", GPU: -1, Cores: []Reading{}, Fans: []Reading{}, Other: []Reading{},
		Zones: []Reading{{Name: "TZ00", Value: round1(fakeHeat)}, {Name: "TZ01", Value: 41.0}}}
	l.Sources = append(l.Sources, "Windows thermal sensor")
	if laptop && fakeHeat > 70 {
		l.CPU.Limit = round1(100 - (fakeHeat-70)*3)
	}
	if !laptop {
		l.Temp.Source = "sensor"
		l.Sources = []string{"LibreHardwareMonitor", "NVIDIA driver"}
		for i := 1; i <= 6; i++ {
			l.Temp.Cores = append(l.Temp.Cores, Reading{Name: "Core #" + itoa(i), Value: round1(fakeHeat - 4 + fakeRnd.Float64()*7)})
		}
		l.Temp.Fans = []Reading{{Name: "CPU fan", Value: math.Round(820 + fakeHeat*11)}, {Name: "System fan #1", Value: 760}}
		l.Temp.Other = []Reading{{Name: "Drive: Composite Temperature", Value: 43}}
		l.CPU.Power = round1(28 + total*0.7)
		l.Temp.GPU = round1(61 + 9*wave)
	}

	memTotal := int64(8 << 30)
	used := 5.3e9 + 4e8*wave
	if !laptop {
		memTotal = 16 << 30
		used = 12.4e9 + 6e8*wave
	}
	l.Mem = LiveMem{Total: memTotal, Used: int64(used), Committed: int64(used * 1.35), CommitLimit: memTotal + 4<<30}

	l.GPU = LiveGPU{Name: "Intel(R) HD Graphics 620", Util: round1(clamp(8+20*wave+spike*0.4, 0, 100)), VRAMUsed: int64(4.6e8 + 1e8*wave), Shared: true, Power: -1, FanPct: -1, ClockMHz: -1, Source: "Windows"}
	if !laptop {
		l.GPU = LiveGPU{Name: "NVIDIA GeForce RTX 3060", Util: round1(clamp(88+9*wave-4*fakeRnd.Float64(), 0, 100)), VRAMUsed: int64(9.1e9 + 6e8*wave), VRAMTotal: 12 << 30,
			Power: round1(148 + 20*wave), FanPct: math.Round(52 + 14*wave), ClockMHz: math.Round(1837 + 40*wave), Source: "NVIDIA"}
	}
	burst := 0.0
	if fakeRnd.Float64() < 0.2 {
		burst = 1
	}
	l.Disk = LiveIO{In: wobble(2.4e6+burst*9e7, 0.6), Out: wobble(6e5+burst*2.4e7, 0.6), Active: round1(clamp(3+burst*55+fakeRnd.Float64()*6, 0, 100)), OK: true}
	l.Net = LiveIO{In: wobble(3.8e5+wave*2.2e6, 0.5), Out: wobble(6e4+wave*2.4e5, 0.5), Active: -1, OK: true}
	if !laptop {
		l.Net.Out = wobble(7.4e5, 0.2) // streaming
	}
	l.Power = LivePower{PowerStatus: powerStatus()}
	if laptop {
		l.Power.Watts = round1(18.4 + 6*wave)
	}

	var groups []Group
	for _, a := range fakeApps() {
		g := Group{Key: a.key, Name: a.desc, Desc: a.exe, Note: notes[a.key], Kind: a.kind, Count: a.count,
			CPU: round1(clamp(wobble(a.cpu*(0.6+0.8*wave), 0.5*a.burst), 0, 100)),
			Mem: int64(wobble(a.mem, 0.03)),
			IO:  math.Round(wobble(a.io, a.burst)),
			GPU: round1(clamp(wobble(a.gpu, 0.15), 0, 100)),
		}
		if g.Name == "" {
			g.Name = friendly[a.key]
		}
		if a.key == "#rigscope" {
			g.Name, g.Desc, g.Note = "Rigscope", "Rigscope.exe and its window", "This app, including the window you're looking at."
		}
		groups = append(groups, g)
	}
	l.Procs = groups
	l.NProcs, l.Threads = 214, 2873
	if !laptop {
		l.NProcs, l.Threads = 231, 3412
	}
	l.UptimeS = 2*86400 + 4*3600 + int64(t)
	if l.CPU.Limit >= 0 && l.CPU.Limit < 90 {
		l.Warnings = append(l.Warnings, "throttled")
	}
	return l
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
