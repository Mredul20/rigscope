package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestCPUTiers(t *testing.T) {
	cases := []struct {
		name           string
		cores, threads int
		tier           int
		win11          string
	}{
		{"Intel(R) Celeron(R) N4020 CPU @ 1.10GHz", 2, 2, 1, "unknown"},
		{"Intel(R) Core(TM) i5-7200U CPU @ 2.50GHz", 2, 4, 1, "no"},
		{"Intel(R) Core(TM) i5-4460 CPU @ 3.20GHz", 4, 4, 2, "no"},
		{"Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz", 4, 8, 3, "yes"},
		{"11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz", 4, 8, 3, "yes"},
		{"12th Gen Intel(R) Core(TM) i5-1235U", 10, 12, 4, "yes"},
		{"Intel(R) Core(TM) i5-10400 CPU @ 2.90GHz", 6, 12, 4, "yes"},
		{"AMD Ryzen 5 5500U with Radeon Graphics", 6, 12, 4, "yes"},
		{"AMD Ryzen 5 3600 6-Core Processor", 6, 12, 4, "yes"},
		{"AMD Ryzen 5 5600X 6-Core Processor", 6, 12, 5, "yes"},
		{"12th Gen Intel(R) Core(TM) i5-12400", 6, 12, 5, "yes"},
		{"AMD Ryzen 7 5800H with Radeon Graphics", 8, 16, 5, "yes"},
		{"AMD Ryzen 7 7840U w/ Radeon 780M Graphics", 8, 16, 5, "yes"},
		{"12th Gen Intel(R) Core(TM) i7-12700H", 14, 20, 6, "yes"},
		{"13th Gen Intel(R) Core(TM) i7-13700K", 16, 24, 6, "yes"},
		{"AMD Ryzen 7 7800X3D 8-Core Processor", 8, 16, 6, "yes"},
		{"Intel(R) Core(TM) Ultra 7 155H", 16, 22, 6, "yes"},
		{"AMD Ryzen AI 9 HX 370 w/ Radeon 890M", 12, 24, 6, "yes"},
		{"AMD Ryzen 7 1700 Eight-Core Processor", 8, 16, 4, "no"},
		{"Intel(R) Core(TM)2 Duo CPU E8400 @ 3.00GHz", 2, 2, 1, "no"},
		{"Intel(R) N100", 4, 4, 2, "yes"},
		{"Snapdragon(R) X Elite - X1E78100 - Qualcomm(R) Oryon(TM) CPU", 12, 12, 6, "yes"},
		{"AMD EPYC 7763 64-Core Processor", 2, 4, 2, "unknown"}, // a 4-thread cloud VM slice
	}
	for _, c := range cases {
		e := estimateCPU(c.name, c.cores, c.threads)
		if e.Tier != c.tier || e.Win11 != c.win11 || !e.Known {
			t.Errorf("%s: tier %d (score %.2f, %s), win11 %s; want tier %d, win11 %s", c.name, e.Tier, e.Score, e.Family, e.Win11, c.tier, c.win11)
		}
	}
}

func TestGPULookup(t *testing.T) {
	cases := map[string]float64{
		"Intel(R) HD Graphics 620":           15,
		"Intel(R) UHD Graphics 620":          16,
		"Intel(R) UHD Graphics":              15,
		"Intel(R) Iris(R) Xe Graphics":       45,
		"Intel(R) Arc(TM) Graphics":          85,
		"AMD Radeon(TM) Graphics":            28,
		"AMD Radeon(TM) Vega 8 Graphics":     26,
		"AMD Radeon 780M Graphics":           95,
		"NVIDIA GeForce GTX 1060 6GB":        100,
		"NVIDIA GeForce GTX 1650":            75,
		"NVIDIA GeForce RTX 3050 Laptop GPU": 115,
		"NVIDIA GeForce RTX 4060 Ti":         310,
		"NVIDIA GeForce MX150":               26,
		"AMD Radeon RX 5600 XT":              165,
		"AMD Radeon RX 560":                  40,
		"AMD Radeon RX 6600M":                175,
		"Radeon RX Vega 56":                  140,
		"Qualcomm(R) Adreno(TM) X1-85 GPU":   70,
		"Microsoft Basic Display Adapter":    3,
	}
	for name, want := range cases {
		g := lookupGPU(name)
		if !g.Found || g.Score != want {
			t.Errorf("%s: got %.1f (found %v, match %q), want %.1f", name, g.Score, g.Found, g.Match, want)
		}
	}
	if g := lookupGPU("NVIDIA GeForce GTX 1650 with Max-Q Design"); g.Score >= 75 {
		t.Errorf("Max-Q laptop GPU should score below the desktop card, got %.1f", g.Score)
	}
}

// Every requirement in the catalog must name hardware Rigscope can score.
func TestCatalogReferences(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range catalog.Items {
		if ids[it.ID] {
			t.Errorf("duplicate id %s", it.ID)
		}
		ids[it.ID] = true
		specs := []Spec{it.Min}
		if it.Rec != nil {
			specs = append(specs, *it.Rec)
		}
		for _, s := range specs {
			if e := estimateCPU(s.CPU.Name, s.CPU.Cores, s.CPU.Threads); !e.Known || s.CPU.Cores == 0 {
				t.Errorf("%s: processor %q isn't recognized", it.ID, s.CPU.Name)
			}
			if !lookupGPU(s.GPU).Found {
				t.Errorf("%s: graphics %q isn't in the GPU table", it.ID, s.GPU)
			}
			if s.RAM <= 0 {
				t.Errorf("%s: memory requirement missing", it.ID)
			}
		}
		if it.Rec != nil && it.Rec.RAM < it.Min.RAM {
			t.Errorf("%s: recommended memory is below the minimum", it.ID)
		}
		if it.Kind != "game" && it.Kind != "app" {
			t.Errorf("%s: kind must be game or app", it.ID)
		}
	}
}

func loadSample(t *testing.T, name string) *rawSpecs {
	b, err := os.ReadFile("testdata/specs-" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := parseSpecs(b)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func sampleBattery(t *testing.T) BatteryInfo {
	b, _ := os.ReadFile("testdata/battery.xml")
	bat := BatteryInfo{Live: PowerStatus{HasBattery: true, Percent: 80}, History: []HistPoint{}, Recent: []UsagePoint{}, Cycles: -1}
	if err := parseBatteryReport(b, &bat); err != nil {
		t.Fatal(err)
	}
	return bat
}

func status(r *Report, id string) string {
	for _, c := range r.Security.Checks {
		if c.ID == id {
			return c.Status
		}
	}
	return "missing"
}

func rating(r *Report, id string) Rating {
	for _, it := range r.Run.Items {
		if it.ID == id {
			return it
		}
	}
	return Rating{}
}

var testNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func TestLaptopReport(t *testing.T) {
	r := buildReport(loadSample(t, "laptop"), sampleBattery(t), false, testNow)
	if !r.Laptop || r.CPU.Tier != 1 || r.CPU.Win11 != "no" || r.Memory.Total != 8<<30 || r.Memory.TypeName != "DDR4" || r.Memory.Speed != 2133 {
		t.Fatalf("laptop basics wrong: %+v %+v", r.CPU, r.Memory)
	}
	if len(r.GPUs) != 1 || !r.GPUs[0].Integrated || r.GPUs[0].Score != 15 {
		t.Fatalf("gpu: %+v", r.GPUs)
	}
	if r.OS.Name != "Windows 11 Pro" || !strings.Contains(r.OS.Friendly, "24H2 (build 26100.6899)") {
		t.Fatalf("os: %+v", r.OS)
	}
	// Windows 11 24H2 Home/Pro support ended 13 October 2026.
	if status(r, "windows") != "bad" || status(r, "secureboot") != "warn" || status(r, "encryption") != "warn" || status(r, "antivirus") != "good" {
		t.Fatalf("security: %+v", r.Security.Checks)
	}
	if status(r, "updates") != "warn" { // last update 75 days before testNow
		t.Fatalf("updates should be warn: %+v", r.Security.Checks)
	}
	if r.Security.Win11OK != "no" {
		t.Fatalf("i5-7200U isn't on the Windows 11 list")
	}
	if v := rating(r, "valorant"); v.Status != "blocked" || !strings.Contains(v.Reasons[0], "Secure Boot") {
		t.Fatalf("valorant should be blocked by Secure Boot: %+v", v)
	}
	if g := rating(r, "gta5"); g.Status != "low" {
		t.Fatalf("gta5: %+v", g)
	}
	if b := rating(r, "browsing"); b.Status != "great" {
		t.Fatalf("browsing: %+v", b)
	}
	if f := rating(r, "fortnite"); f.Status != "ok" {
		t.Fatalf("fortnite: %+v", f)
	}
	if a := rating(r, "androidstudio"); a.Status == "blocked" {
		t.Fatalf("android studio: virtualization is on: %+v", a)
	}
	if !strings.Contains(r.Run.Summary, "very basic processor") {
		t.Fatalf("summary: %s", r.Run.Summary)
	}
}

func TestDesktopReport(t *testing.T) {
	raw := loadSample(t, "desktop")
	r := buildReport(raw, BatteryInfo{Live: PowerStatus{}, History: []HistPoint{}, Recent: []UsagePoint{}, Cycles: -1}, true, testNow)
	if r.Laptop || len(r.GPUs) != 1 || r.GPUs[0].Name != "NVIDIA GeForce RTX 3060" || r.Run.GPUTier != 5 || r.Run.CPUTier != 5 {
		t.Fatalf("desktop basics: laptop=%v gpus=%+v run=%+v", r.Laptop, r.GPUs, r.Run)
	}
	if r.Memory.Channels != "dual" || r.Memory.Total != 16<<30 {
		t.Fatalf("memory: %+v", r.Memory)
	}
	for _, id := range []string{"windows", "secureboot", "tpm", "encryption", "memoryintegrity", "updates", "firewall"} {
		if s := status(r, id); s != "good" {
			t.Errorf("%s = %s, want good", id, s)
		}
	}
	if r.Security.Win11OK != "yes" {
		t.Errorf("Ryzen 5 5600X with TPM 2.0 should meet Windows 11 requirements")
	}
	for _, id := range []string{"valorant", "cs2", "fortnite", "eldenring", "rdr2", "fc", "gta5"} {
		if s := rating(r, id).Status; s != "great" && s != "ok" {
			t.Errorf("%s on a Ryzen 5 5600X + RTX 3060: %s", id, s)
		}
	}
	if s := rating(r, "battlefield6").Status; s != "great" && s != "ok" {
		t.Errorf("battlefield6: %s %+v", s, rating(r, "battlefield6"))
	}
	if !r.CPU.Virt {
		t.Errorf("hypervisor present means virtualization is on")
	}
}

func TestWindowsSupportDates(t *testing.T) {
	cases := []struct {
		build   int
		edition string
		want    string
	}{
		{26100, "Professional", "bad"},      // 24H2 Home/Pro ended 2026-10-13
		{26100, "Enterprise", "good"},       // until 2027-10-12
		{26200, "Core", "good"},             // 25H2
		{22631, "Enterprise", "warn"},       // 23H2 Enterprise ends 2026-11-10, under 75 days away
		{19045, "Professional", "warn"},     // Windows 10 22H2: ESU possible
		{19044, "Professional", "bad"},      // older Windows 10
		{19044, "EnterpriseS", "good"},      // LTSC 2021 until 2027-01-12
		{26500, "Core", "unknown"},          // not in the list
		{26100, "ServerDatacenter", "info"}, // Windows Server has its own schedule
	}
	for _, c := range cases {
		got := windowsSupport(rawOS{Build: c.build, Edition: c.edition}, testNow)
		if got.Status != c.want {
			t.Errorf("build %d %s: %s (%s), want %s", c.build, c.edition, got.Status, got.Detail, c.want)
		}
	}
}

func TestBatteryParse(t *testing.T) {
	b := sampleBattery(t)
	if !b.ReportOK || b.Design != 41990 || len(b.History) == 0 {
		t.Fatalf("battery: %+v", b)
	}
}

func TestAVState(t *testing.T) {
	if !avOn(397568) || avOutdated(397568) { // 0x061100: on, up to date
		t.Error("397568 should be on and up to date")
	}
	if avOn(393472) { // 0x060100: off
		t.Error("393472 should be off")
	}
	if !avOn(397584) || !avOutdated(397584) { // 0x061110: on, outdated
		t.Error("397584 should be on and outdated")
	}
}
