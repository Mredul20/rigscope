//go:build windows

package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

//go:embed ps/sensors.ps1
var sensorsScript string

var (
	ntdll                        = syscall.NewLazyDLL("ntdll.dll")
	pdhDLL                       = syscall.NewLazyDLL("pdh.dll")
	user32                       = syscall.NewLazyDLL("user32.dll")
	versionDLL                   = syscall.NewLazyDLL("version.dll")
	powrprof                     = syscall.NewLazyDLL("powrprof.dll")
	pNtQuerySystemInformation    = ntdll.NewProc("NtQuerySystemInformation")
	pPdhOpenQuery                = pdhDLL.NewProc("PdhOpenQueryW")
	pPdhAddEnglishCounter        = pdhDLL.NewProc("PdhAddEnglishCounterW")
	pPdhCollectQueryData         = pdhDLL.NewProc("PdhCollectQueryData")
	pPdhGetFormattedCounterArray = pdhDLL.NewProc("PdhGetFormattedCounterArrayW")
	pEnumWindows                 = user32.NewProc("EnumWindows")
	pIsWindowVisible             = user32.NewProc("IsWindowVisible")
	pGetWindowTextLength         = user32.NewProc("GetWindowTextLengthW")
	pGetWindowThreadProcessId    = user32.NewProc("GetWindowThreadProcessId")
	pGetWindow                   = user32.NewProc("GetWindow")
	pQueryFullProcessImageName   = kernel32.NewProc("QueryFullProcessImageNameW")
	pGetTickCount64              = kernel32.NewProc("GetTickCount64")
	pCreateJobObject             = kernel32.NewProc("CreateJobObjectW")
	pSetInformationJobObject     = kernel32.NewProc("SetInformationJobObject")
	pAssignProcessToJobObject    = kernel32.NewProc("AssignProcessToJobObject")
	pGetCurrentProcess           = kernel32.NewProc("GetCurrentProcess")
	pGetFileVersionInfoSize      = versionDLL.NewProc("GetFileVersionInfoSizeW")
	pGetFileVersionInfo          = versionDLL.NewProc("GetFileVersionInfoW")
	pVerQueryValue               = versionDLL.NewProc("VerQueryValueW")
	pCallNtPowerInformation      = powrprof.NewProc("CallNtPowerInformation")
)

func init() {
	// Put Rigscope in a job that ends all its helper processes when it exits,
	// even if it crashes, so nothing is left running in the background.
	job, _, _ := pCreateJobObject.Call(0, 0)
	if job == 0 {
		return
	}
	var info [144]byte                               // JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	binary.LittleEndian.PutUint32(info[16:], 0x2000) // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	pSetInformationJobObject.Call(job, 9, uintptr(unsafe.Pointer(&info[0])), uintptr(len(info)))
	self, _, _ := pGetCurrentProcess.Call()
	pAssignProcessToJobObject.Call(job, self)
}

// ---- PDH performance counters ----
// English counter names work on every Windows language.

const (
	pdhFmtDouble    = 0x00000200
	pdhFmtNoCap100  = 0x00008000
	pdhMoreData     = 0x800007D2
	pdhCStatusValid = 0
	pdhCStatusNew   = 1
)

type pdh struct {
	q    uintptr
	ctrs map[string]uintptr
	buf  []byte
}

func newPDH() *pdh {
	p := &pdh{ctrs: map[string]uintptr{}}
	if err := pPdhOpenQuery.Find(); err != nil {
		return p
	}
	if r, _, _ := pPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&p.q))); r != 0 {
		p.q = 0
	}
	return p
}

func (p *pdh) add(key, path string) {
	if p.q == 0 {
		return
	}
	var h uintptr
	if r, _, _ := pPdhAddEnglishCounter.Call(p.q, uintptr(unsafe.Pointer(u16(path))), 0, uintptr(unsafe.Pointer(&h))); r == 0 {
		p.ctrs[key] = h
	}
}

func (p *pdh) collect() {
	if p.q != 0 {
		pPdhCollectQueryData.Call(p.q)
	}
}

// utf16In reads a NUL-terminated UTF-16 string that ptr points to inside buf.
// Windows APIs often return pointers into the buffer you gave them.
func utf16In(buf []byte, ptr uintptr) string {
	if len(buf) == 0 || ptr == 0 {
		return ""
	}
	base := uintptr(unsafe.Pointer(&buf[0]))
	if ptr < base || ptr >= base+uintptr(len(buf)) {
		return ""
	}
	var u []uint16
	for i := int(ptr - base); i+1 < len(buf); i += 2 {
		c := binary.LittleEndian.Uint16(buf[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// values returns instance name -> value for a counter. Single-instance counters use the key "".
func (p *pdh) values(key string) (map[string]float64, bool) {
	h, ok := p.ctrs[key]
	if !ok {
		return nil, false
	}
	var size, count uint32
	r, _, _ := pPdhGetFormattedCounterArray.Call(h, pdhFmtDouble|pdhFmtNoCap100, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) != pdhMoreData || size == 0 {
		return nil, false
	}
	if cap(p.buf) < int(size) {
		p.buf = make([]byte, size)
	}
	buf := p.buf[:size]
	r, _, _ = pPdhGetFormattedCounterArray.Call(h, pdhFmtDouble|pdhFmtNoCap100, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return nil, false
	}
	out := make(map[string]float64, count)
	for i := 0; i < int(count) && (i+1)*24 <= len(buf); i++ {
		item := buf[i*24:] // PDH_FMT_COUNTERVALUE_ITEM_W on 64-bit: name pointer, status, padding, double
		status := binary.LittleEndian.Uint32(item[8:])
		if status != pdhCStatusValid && status != pdhCStatusNew {
			continue
		}
		name := utf16In(buf, uintptr(binary.LittleEndian.Uint64(item[0:])))
		out[name] = math.Float64frombits(binary.LittleEndian.Uint64(item[16:]))
	}
	return out, true
}

func sum(m map[string]float64) float64 {
	t := 0.0
	for _, v := range m {
		t += v
	}
	return t
}

// ---- processor times per core (works without performance counters) ----

type coreTimes struct{ idle, kernel, user int64 }

func readCoreTimes() []coreTimes {
	n := runtime.NumCPU()
	buf := make([]byte, 48*n)
	var ret uint32
	r, _, _ := pNtQuerySystemInformation.Call(8, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&ret)))
	if r != 0 {
		return nil
	}
	cnt := int(ret) / 48
	out := make([]coreTimes, cnt)
	for i := range out {
		b := buf[i*48:]
		out[i] = coreTimes{int64(binary.LittleEndian.Uint64(b[0:])), int64(binary.LittleEndian.Uint64(b[8:])), int64(binary.LittleEndian.Uint64(b[16:]))}
	}
	return out
}

// ---- processes ----

type procSample struct {
	name       string
	ppid       uint32
	create     int64
	cpuTime    int64
	ioBytes    int64
	privateWS  int64
	workingSet int64
	threads    uint32
}

func readProcesses() (map[uint32]procSample, int) {
	size := 1 << 20
	for tries := 0; tries < 6; tries++ {
		buf := make([]byte, size)
		var ret uint32
		r, _, _ := pNtQuerySystemInformation.Call(5, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&ret)))
		if uint32(r) == 0xC0000004 { // STATUS_INFO_LENGTH_MISMATCH
			size = int(ret) + 256<<10
			if size < len(buf)*2 {
				size = len(buf) * 2
			}
			continue
		}
		if r != 0 {
			return nil, 0
		}
		base := uintptr(unsafe.Pointer(&buf[0]))
		out := map[uint32]procSample{}
		threads := 0
		for off := 0; off+0x100 <= len(buf); {
			b := buf[off:]
			next := binary.LittleEndian.Uint32(b[0:])
			nlen := int(binary.LittleEndian.Uint16(b[0x38:]))
			nptr := uintptr(binary.LittleEndian.Uint64(b[0x40:]))
			name := ""
			if nptr >= base && int(nptr-base)+nlen <= len(buf) {
				s := int(nptr - base)
				u := make([]uint16, nlen/2)
				for i := range u {
					u[i] = binary.LittleEndian.Uint16(buf[s+i*2:])
				}
				name = string(utf16.Decode(u))
			}
			pid := uint32(binary.LittleEndian.Uint64(b[0x50:]))
			ps := procSample{
				name:       name,
				ppid:       uint32(binary.LittleEndian.Uint64(b[0x58:])),
				threads:    binary.LittleEndian.Uint32(b[0x04:]),
				privateWS:  int64(binary.LittleEndian.Uint64(b[0x08:])),
				create:     int64(binary.LittleEndian.Uint64(b[0x20:])),
				cpuTime:    int64(binary.LittleEndian.Uint64(b[0x28:])) + int64(binary.LittleEndian.Uint64(b[0x30:])),
				workingSet: int64(binary.LittleEndian.Uint64(b[0x90:])),
				ioBytes:    int64(binary.LittleEndian.Uint64(b[0xE8:])) + int64(binary.LittleEndian.Uint64(b[0xF0:])),
			}
			if pid == 0 {
				ps.name = "System Idle Process"
			}
			threads += int(ps.threads)
			out[pid] = ps
			if next == 0 {
				break
			}
			off += int(next)
		}
		return out, threads
	}
	return nil, 0
}

type exeInfo struct{ path, desc string }

var (
	exeMu    sync.Mutex
	exeCache = map[string]exeInfo{} // "pid:createtime" -> info
	verCache = map[string]string{}  // path -> description
)

func processPath(pid uint32) string {
	h, err := syscall.OpenProcess(0x1000, false, pid) // PROCESS_QUERY_LIMITED_INFORMATION
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := pQueryFullProcessImageName.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func fileDescription(path string) string {
	if path == "" {
		return ""
	}
	if d, ok := verCache[path]; ok {
		return d
	}
	desc := ""
	p := u16(path)
	size, _, _ := pGetFileVersionInfoSize.Call(uintptr(unsafe.Pointer(p)), 0)
	if size > 0 && size < 1<<22 {
		data := make([]byte, size)
		if r, _, _ := pGetFileVersionInfo.Call(uintptr(unsafe.Pointer(p)), 0, size, uintptr(unsafe.Pointer(&data[0]))); r != 0 {
			var tr uintptr
			var trLen uint32
			langs := []string{}
			if r, _, _ := pVerQueryValue.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(u16(`\VarFileInfo\Translation`))), uintptr(unsafe.Pointer(&tr)), uintptr(unsafe.Pointer(&trLen))); r != 0 && trLen >= 4 {
				if base := uintptr(unsafe.Pointer(&data[0])); tr >= base && int(tr-base)+4 <= len(data) {
					i := int(tr - base)
					lang, cp := binary.LittleEndian.Uint16(data[i:]), binary.LittleEndian.Uint16(data[i+2:])
					langs = append(langs, strconv.FormatUint(uint64(lang)<<16|uint64(cp), 16))
				}
			}
			langs = append(langs, "040904b0", "040904e4", "000004b0")
			for _, l := range langs {
				for len(l) < 8 {
					l = "0" + l
				}
				var v uintptr
				var vl uint32
				if r, _, _ := pVerQueryValue.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(u16(`\StringFileInfo\`+l+`\FileDescription`))), uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&vl))); r != 0 && vl > 1 {
					desc = strings.TrimSpace(utf16In(data, v))
					break
				}
			}
			runtime.KeepAlive(data)
		}
	}
	verCache[path] = desc
	return desc
}

func lookupExe(pid uint32, create int64) exeInfo {
	key := strconv.FormatUint(uint64(pid), 10) + ":" + strconv.FormatInt(create, 10)
	exeMu.Lock()
	defer exeMu.Unlock()
	if len(exeCache) > 4000 { // processes come and go; start over now and then
		exeCache = map[string]exeInfo{}
	}
	if e, ok := exeCache[key]; ok {
		return e
	}
	path := processPath(pid)
	e := exeInfo{path: path, desc: fileDescription(path)}
	exeCache[key] = e
	return e
}

// windowedPIDs lists processes that have a visible, titled, top-level window: the "apps" in Task Manager.
// Windows allows only a limited number of callbacks per program, so one is created and reused.
var (
	enumMu   sync.Mutex
	enumSeen map[uint32]bool
	enumCB   = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if v, _, _ := pIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		if owner, _, _ := pGetWindow.Call(hwnd, 4); owner != 0 { // GW_OWNER: skip dialogs owned by another window
			return 1
		}
		if n, _, _ := pGetWindowTextLength.Call(hwnd); n == 0 {
			return 1
		}
		var pid uint32
		pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		enumSeen[pid] = true
		return 1
	})
)

func windowedPIDs() map[uint32]bool {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumSeen = map[uint32]bool{}
	pEnumWindows.Call(enumCB, 0)
	return enumSeen
}

var windowsProcs = map[string]bool{"system": true, "registry": true, "memory compression": true, "secure system": true,
	"system idle process": true, "smss.exe": true, "csrss.exe": true, "wininit.exe": true, "services.exe": true, "lsass.exe": true,
	"svchost.exe": true, "winlogon.exe": true, "fontdrvhost.exe": true, "dwm.exe": true, "lsaiso.exe": true}

// ---- helper processes: sensors and NVIDIA ----

type sensorsMsg struct {
	App     string          `json:"app"`
	Sensors []sensorReading `json:"sensors"`
	ACPI    []Reading       `json:"acpi"`
	Battery *struct {
		Charge    int64 `json:"charge"`
		Discharge int64 `json:"discharge"`
	} `json:"battery"`
}

type nvidiaMsg struct {
	name                        string
	temp, util, memUsed, memTot float64
	power, fan, clock           float64
}

var (
	helperMu   sync.Mutex
	sensorsNow *sensorsMsg
	sensorsAt  time.Time
	nvidiaNow  *nvidiaMsg
	nvidiaAt   time.Time
	helperPIDs = map[uint32]bool{}
	sensorOnce sync.Once
)

func startHelper(name string, args []string, onLine func(string)) {
	cmd := hiddenCmd(context.Background(), name, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if cmd.Start() != nil {
		return
	}
	helperMu.Lock()
	helperPIDs[uint32(cmd.Process.Pid)] = true
	helperMu.Unlock()
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 1<<16), 1<<22)
		for sc.Scan() {
			onLine(sc.Text())
		}
		cmd.Wait()
	}()
}

func nvidiaSMI() string {
	for _, p := range []string{systemExe(`System32\nvidia-smi.exe`), filepath.Join(os.Getenv("ProgramFiles"), `NVIDIA Corporation\NVSMI\nvidia-smi.exe`)} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func parseNum(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return -1
	}
	return f
}

func startSensors() {
	sensorOnce.Do(func() {
		u := utf16.Encode([]rune(sensorsScript))
		b := make([]byte, len(u)*2)
		for i, c := range u {
			b[2*i], b[2*i+1] = byte(c), byte(c>>8)
		}
		startHelper(systemExe(`System32\WindowsPowerShell\v1.0\powershell.exe`),
			[]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(b)},
			func(line string) {
				var m sensorsMsg
				if json.Unmarshal([]byte(line), &m) == nil {
					helperMu.Lock()
					sensorsNow, sensorsAt = &m, time.Now()
					helperMu.Unlock()
				}
			})
		if smi := nvidiaSMI(); smi != "" {
			startHelper(smi, []string{"--query-gpu=name,temperature.gpu,utilization.gpu,memory.used,memory.total,power.draw,fan.speed,clocks.gr",
				"--format=csv,noheader,nounits", "-l", "2"}, func(line string) {
				f := strings.Split(line, ",")
				if len(f) < 8 {
					return
				}
				m := &nvidiaMsg{name: strings.TrimSpace(f[0]), temp: parseNum(f[1]), util: parseNum(f[2]), memUsed: parseNum(f[3]),
					memTot: parseNum(f[4]), power: parseNum(f[5]), fan: parseNum(f[6]), clock: parseNum(f[7])}
				helperMu.Lock()
				if nvidiaNow == nil || time.Since(nvidiaAt) > time.Second || m.util > nvidiaNow.util {
					nvidiaNow, nvidiaAt = m, time.Now() // with several NVIDIA cards, keep the busiest
				}
				helperMu.Unlock()
			})
		}
	})
}

// ---- the sampler ----

type sampler struct {
	once       sync.Once
	pdh        *pdh
	prevCores  []coreTimes
	prevProcs  map[uint32]procSample
	prevAt     time.Time
	windowed   map[uint32]bool
	windowedAt time.Time
	baseMHz    float64
}

var smp = &sampler{}
var reGPUInst = regexp.MustCompile(`pid_(\d+)_.*engtype_([A-Za-z0-9 ]+)`)

func (s *sampler) init() {
	s.pdh = newPDH()
	q := s.pdh
	q.add("perf", `\Processor Information(_Total)\% Processor Performance`)
	q.add("freq", `\Processor Information(_Total)\Processor Frequency`)
	q.add("limit", `\Processor Information(_Total)\% Performance Limit`)
	q.add("tzh", `\Thermal Zone Information(*)\High Precision Temperature`)
	q.add("tz", `\Thermal Zone Information(*)\Temperature`)
	q.add("dr", `\PhysicalDisk(_Total)\Disk Read Bytes/sec`)
	q.add("dw", `\PhysicalDisk(_Total)\Disk Write Bytes/sec`)
	q.add("didle", `\PhysicalDisk(_Total)\% Idle Time`)
	q.add("nr", `\Network Interface(*)\Bytes Received/sec`)
	q.add("ns", `\Network Interface(*)\Bytes Sent/sec`)
	q.add("gpu", `\GPU Engine(*)\Utilization Percentage`)
	q.add("gded", `\GPU Adapter Memory(*)\Dedicated Usage`)
	q.add("gsh", `\GPU Adapter Memory(*)\Shared Usage`)
	q.collect()
	var info [64 * 24]byte // PROCESSOR_POWER_INFORMATION for up to 64 processors
	if r, _, _ := pCallNtPowerInformation.Call(11, 0, 0, uintptr(unsafe.Pointer(&info[0])), uintptr(len(info))); r == 0 {
		s.baseMHz = float64(binary.LittleEndian.Uint32(info[4:])) // MaxMhz is the rated speed
	}
	s.prevCores = readCoreTimes()
	s.prevProcs, _ = readProcesses()
	s.prevAt = time.Now()
}

var sampleMu sync.Mutex

func sampleLive() *Live {
	sampleMu.Lock()
	defer sampleMu.Unlock()
	s := smp
	first := false
	s.once.Do(func() { s.init(); first = true })
	if first {
		time.Sleep(400 * time.Millisecond) // rates need two readings
	}
	now := time.Now()
	dt := now.Sub(s.prevAt).Seconds()
	if dt <= 0 {
		dt = 1
	}
	l := &Live{Time: now.UnixMilli(), Sources: []string{}, Warnings: []string{}, Procs: []Group{}}
	s.pdh.collect()

	// processor load per core
	cores := readCoreTimes()
	if len(cores) == len(s.prevCores) && len(cores) > 0 {
		var tBusy, tAll float64
		for i := range cores {
			d := cores[i]
			p := s.prevCores[i]
			all := float64((d.kernel - p.kernel) + (d.user - p.user)) // kernel time includes idle
			idle := float64(d.idle - p.idle)
			u := 0.0
			if all > 0 {
				u = (1 - idle/all) * 100
			}
			u = clamp(u, 0, 100)
			l.CPU.Cores = append(l.CPU.Cores, round1(u))
			tBusy += all - idle
			tAll += all
		}
		if tAll > 0 {
			l.CPU.Total = round1(clamp((tBusy/tAll)*100, 0, 100))
		}
	}
	s.prevCores = cores
	l.CPU.BaseMHz, l.CPU.Limit, l.CPU.Power = s.baseMHz, -1, -1
	if lr := reportSnap.Load(); lr != nil && lr.CPU.BaseMHz > 0 {
		l.CPU.BaseMHz = float64(lr.CPU.BaseMHz)
	}
	if perf, ok := s.pdh.values("perf"); ok {
		f, _ := s.pdh.values("freq")
		if p, ok := perf["_Total"]; ok {
			base := f["_Total"]
			if base <= 0 {
				base = l.CPU.BaseMHz
			}
			l.CPU.MHz = round1(base * p / 100)
		}
	}
	if lim, ok := s.pdh.values("limit"); ok {
		if v, ok := lim["_Total"]; ok {
			l.CPU.Limit = round1(v)
		}
	}

	// memory
	m := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, _ := pGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r != 0 {
		l.Mem = LiveMem{Total: int64(m.TotalPhys), Used: int64(m.TotalPhys - m.AvailPhys), CommitLimit: int64(m.TotalPageFile), Committed: int64(m.TotalPageFile - m.AvailPageFile)}
	}

	// disk and network
	if r, ok := s.pdh.values("dr"); ok {
		w, _ := s.pdh.values("dw")
		l.Disk = LiveIO{In: r["_Total"], Out: w["_Total"], Active: -1, OK: true}
		if idle, ok := s.pdh.values("didle"); ok {
			if v, ok := idle["_Total"]; ok {
				l.Disk.Active = round1(clamp(100-v, 0, 100))
			}
		}
	}
	if r, ok := s.pdh.values("nr"); ok {
		w, _ := s.pdh.values("ns")
		l.Net = LiveIO{In: sum(r), Out: sum(w), Active: -1, OK: true}
	}

	// graphics, per engine type and per process
	l.GPU = LiveGPU{Util: -1, VRAMUsed: -1, Power: -1, FanPct: -1, ClockMHz: -1}
	gpuByPID := map[uint32]map[string]float64{}
	if eng, ok := s.pdh.values("gpu"); ok {
		byType := map[string]float64{}
		for inst, v := range eng {
			mm := reGPUInst.FindStringSubmatch(inst)
			if mm == nil {
				continue
			}
			pid64, _ := strconv.ParseUint(mm[1], 10, 32)
			et := mm[2]
			byType[et] += v
			if gpuByPID[uint32(pid64)] == nil {
				gpuByPID[uint32(pid64)] = map[string]float64{}
			}
			gpuByPID[uint32(pid64)][et] += v
		}
		best := 0.0
		for _, v := range byType {
			if v > best {
				best = v
			}
		}
		l.GPU.Util = round1(clamp(best, 0, 100))
		l.GPU.Source = "Windows"
	}
	if lr := reportSnap.Load(); lr != nil && len(lr.GPUs) > 0 {
		g := lr.GPUs[0]
		l.GPU.Name, l.GPU.Shared = g.Name, g.Integrated
		if !g.Integrated {
			l.GPU.VRAMTotal = g.Memory
		}
	}
	if d, ok := s.pdh.values("gded"); ok {
		var maxD float64
		for _, v := range d {
			if v > maxD {
				maxD = v
			}
		}
		if l.GPU.Shared {
			if sh, ok := s.pdh.values("gsh"); ok {
				for _, v := range sh {
					if v > maxD {
						maxD = v
					}
				}
			}
		}
		l.GPU.VRAMUsed = int64(maxD)
	}

	// temperatures
	l.Temp = LiveTemp{CPU: -1, GPU: -1, Source: "none", Cores: []Reading{}, Zones: []Reading{}, Fans: []Reading{}, Other: []Reading{}}
	if tz, ok := s.pdh.values("tzh"); ok && len(tz) > 0 {
		for n, v := range tz {
			l.Temp.Zones = append(l.Temp.Zones, Reading{Name: zoneName(n), Value: round1(v/10 - 273.15)})
		}
	} else if tz, ok := s.pdh.values("tz"); ok {
		for n, v := range tz {
			l.Temp.Zones = append(l.Temp.Zones, Reading{Name: zoneName(n), Value: round1(v - 273.15)})
		}
	}
	helperMu.Lock()
	sn, nv := sensorsNow, nvidiaNow
	if time.Since(sensorsAt) > 10*time.Second {
		sn = nil
	}
	if time.Since(nvidiaAt) > 10*time.Second {
		nv = nil
	}
	helpers := make(map[uint32]bool, len(helperPIDs))
	for k := range helperPIDs {
		helpers[k] = true
	}
	helperMu.Unlock()
	if len(l.Temp.Zones) == 0 && sn != nil {
		l.Temp.Zones = append(l.Temp.Zones, sn.ACPI...)
	}
	sort.Slice(l.Temp.Zones, func(i, j int) bool { return l.Temp.Zones[i].Name < l.Temp.Zones[j].Name })
	if sn != nil && len(sn.Sensors) > 0 {
		if pkg, cores, ok := pickCPUTemp(sn.Sensors); ok {
			l.Temp.CPU, l.Temp.Cores, l.Temp.Source = round1(pkg), cores, "sensor"
			l.Sources = append(l.Sources, sn.App)
		}
		for _, r := range sn.Sensors {
			p := strings.ToLower(r.Parent)
			switch {
			case r.Type == "Fan" && r.Value > 0:
				l.Temp.Fans = append(l.Temp.Fans, Reading{Name: r.Name, Value: r.Value})
			case r.Type == "Power" && strings.Contains(p, "cpu") && strings.EqualFold(r.Name, "CPU Package") && r.Value > 0:
				l.CPU.Power = round1(r.Value)
			case r.Type == "Temperature" && strings.Contains(p, "gpu") && (strings.EqualFold(r.Name, "GPU Core") || strings.EqualFold(r.Name, "GPU Hot Spot") && l.Temp.GPU < 0) && r.Value > 0:
				l.Temp.GPU = round1(r.Value)
			case r.Type == "Temperature" && (strings.Contains(p, "nvme") || strings.Contains(p, "hdd") || strings.Contains(p, "ssd") || strings.Contains(p, "storage")) && r.Value > 0:
				l.Temp.Other = append(l.Temp.Other, Reading{Name: "Drive: " + r.Name, Value: round1(r.Value)})
			}
		}
	}
	if l.Temp.Source == "none" {
		if z := validZones(l.Temp.Zones); len(z) > 0 {
			best := z[0]
			for _, r := range z {
				if strings.Contains(strings.ToUpper(r.Name), "CPU") {
					best = r
					break
				}
				if r.Value > best.Value {
					best = r
				}
			}
			l.Temp.CPU, l.Temp.Source = best.Value, "thermalzone"
			l.Sources = append(l.Sources, "Windows thermal sensor")
		}
	}
	if nv != nil {
		if nv.temp > 0 {
			l.Temp.GPU = nv.temp
		}
		if nv.util >= 0 {
			l.GPU.Util = nv.util
		}
		if nv.memUsed >= 0 {
			l.GPU.VRAMUsed = int64(nv.memUsed * (1 << 20))
		}
		if nv.memTot > 0 {
			l.GPU.VRAMTotal = int64(nv.memTot * (1 << 20))
		}
		l.GPU.Power, l.GPU.FanPct, l.GPU.ClockMHz, l.GPU.Source = nv.power, nv.fan, nv.clock, "NVIDIA"
		if l.GPU.Name == "" {
			l.GPU.Name = nv.name
		}
		l.Sources = append(l.Sources, "NVIDIA driver")
	}

	// battery
	l.Power = LivePower{PowerStatus: powerStatus()}
	if sn != nil && sn.Battery != nil {
		if sn.Battery.Charge > 0 {
			l.Power.Watts = round1(float64(sn.Battery.Charge) / 1000)
		} else if sn.Battery.Discharge > 0 {
			l.Power.Watts = -round1(float64(sn.Battery.Discharge) / 1000)
		}
	}

	// processes
	procs, threads := readProcesses()
	l.NProcs, l.Threads = len(procs), threads
	if time.Since(s.windowedAt) > 3*time.Second {
		s.windowed, s.windowedAt = windowedPIDs(), now
	}
	ncpu := float64(runtime.NumCPU())
	sysRoot := strings.ToLower(os.Getenv("SystemRoot"))
	var raws []rawProc
	for pid, p := range procs {
		if pid == 0 {
			continue
		}
		rp := rawProc{PID: pid, PPID: p.ppid, Name: p.name, Mem: p.privateWS, Windowed: s.windowed[pid]}
		if rp.Mem <= 0 {
			rp.Mem = p.workingSet
		}
		if prev, ok := s.prevProcs[pid]; ok && prev.create == p.create {
			rp.CPU = clamp(float64(p.cpuTime-prev.cpuTime)/(dt*1e7*ncpu)*100, 0, 100)
			if d := p.ioBytes - prev.ioBytes; d > 0 {
				rp.IO = float64(d) / dt
			}
		}
		if g := gpuByPID[pid]; g != nil {
			for _, v := range g {
				if v > rp.GPU {
					rp.GPU = v
				}
			}
		}
		ln := strings.ToLower(p.name)
		if pid == 4 || windowsProcs[ln] {
			rp.System = true
		} else {
			e := lookupExe(pid, p.create)
			rp.Path, rp.Desc = e.path, e.desc
			if sysRoot != "" && strings.HasPrefix(strings.ToLower(e.path), sysRoot+`\`) && !rp.Windowed {
				rp.System = true
			}
		}
		raws = append(raws, rp)
	}
	self := descendants(raws, uint32(os.Getpid()))
	for pid := range helpers {
		self[pid] = true
	}
	l.Procs = groupProcs(raws, self, runtime.NumCPU())
	s.prevProcs, s.prevAt = procs, now

	if t, _, _ := pGetTickCount64.Call(); t > 0 {
		l.UptimeS = int64(t / 1000)
	}
	if l.CPU.Limit >= 0 && l.CPU.Limit < 90 {
		l.Warnings = append(l.Warnings, "throttled")
	}
	return l
}

func zoneName(inst string) string {
	n := strings.TrimPrefix(inst, `\_TZ.`)
	return strings.TrimPrefix(n, "_TZ.")
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
