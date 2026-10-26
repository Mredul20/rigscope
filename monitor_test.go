package main

import "testing"

func TestGroupProcs(t *testing.T) {
	procs := []rawProc{
		{PID: 10, PPID: 1, Name: "chrome.exe", Desc: "Google Chrome", CPU: 3, Mem: 100, Windowed: true},
		{PID: 11, PPID: 10, Name: "chrome.exe", Desc: "Google Chrome", CPU: 2, Mem: 50},
		{PID: 4, Name: "System", CPU: 1, System: true},
		{PID: 20, PPID: 1, Name: "MsMpEng.exe", Desc: "Antimalware Service Executable", CPU: 7, Mem: 30},
		{PID: 30, PPID: 1, Name: "rigscope.exe", CPU: 0.4, Mem: 20},
		{PID: 31, PPID: 30, Name: "msedge.exe", CPU: 0.6, Mem: 80},
		{PID: 32, PPID: 31, Name: "msedge.exe", CPU: 0.2, Mem: 40},
		{PID: 40, PPID: 1, Name: "msedge.exe", Desc: "Microsoft Edge", CPU: 1, Mem: 300, Windowed: true},
	}
	self := descendants(procs, 30)
	if !self[31] || !self[32] || self[40] {
		t.Fatalf("descendants: %v", self)
	}
	g := groupProcs(procs, self, 4)
	byKey := map[string]Group{}
	for _, x := range g {
		byKey[x.Key] = x
	}
	if c := byKey["chrome.exe"]; c.Count != 2 || c.CPU != 5 || c.Mem != 150 || c.Kind != "app" || c.Name != "Google Chrome" {
		t.Errorf("chrome group: %+v", c)
	}
	if r := byKey["#rigscope"]; r.Count != 3 || r.Kind != "self" || r.Name != "Rigscope" {
		t.Errorf("rigscope group should hold its own window: %+v", r)
	}
	if e := byKey["msedge.exe"]; e.Count != 1 || e.Kind != "app" {
		t.Errorf("the person's own Edge stays separate: %+v", e)
	}
	if s := byKey["system"]; s.Kind != "windows" || s.Name != "System" || s.Note == "" {
		t.Errorf("system: %+v", s)
	}
	if g[0].Key != "msmpeng.exe" {
		t.Errorf("busiest first, got %s", g[0].Key)
	}
}

func TestPickCPUTemp(t *testing.T) {
	intel := []sensorReading{
		{Name: "Core #2", Type: "Temperature", Parent: "/intelcpu/0", Value: 61},
		{Name: "Core #1", Type: "Temperature", Parent: "/intelcpu/0", Value: 58},
		{Name: "CPU Package", Type: "Temperature", Parent: "/intelcpu/0", Value: 64},
		{Name: "GPU Core", Type: "Temperature", Parent: "/gpu-nvidia/0", Value: 70},
		{Name: "CPU Package", Type: "Power", Parent: "/intelcpu/0", Value: 35},
	}
	pkg, cores, ok := pickCPUTemp(intel)
	if !ok || pkg != 64 || len(cores) != 2 || cores[0].Name != "Core #1" {
		t.Fatalf("intel: %v %v %v", pkg, cores, ok)
	}
	amd := []sensorReading{{Name: "Core (Tctl/Tdie)", Type: "Temperature", Parent: "/amdcpu/0", Value: 71.5}, {Name: "CCD1 (Tdie)", Type: "Temperature", Parent: "/amdcpu/0", Value: 66}}
	if pkg, _, ok := pickCPUTemp(amd); !ok || pkg != 71.5 {
		t.Fatalf("amd: %v", pkg)
	}
	if _, _, ok := pickCPUTemp([]sensorReading{{Name: "GPU Core", Type: "Temperature", Parent: "/gpu/0", Value: 50}}); ok {
		t.Fatal("no processor sensor should mean no reading")
	}
}

func TestValidZones(t *testing.T) {
	z := validZones([]Reading{{"TZ00", 27.8}, {"TZ01", 0}, {"TZ02", 63.1}, {"TZ03", 200}})
	if len(z) != 1 || z[0].Name != "TZ02" {
		t.Fatalf("zones: %v", z)
	}
}

func TestSampleLive(t *testing.T) {
	l := sampleLive()
	if len(l.CPU.Cores) == 0 || l.Mem.Total == 0 || len(l.Procs) == 0 {
		t.Fatalf("sample: %+v", l)
	}
}
