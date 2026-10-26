//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	shell32               = syscall.NewLazyDLL("shell32.dll")
	pGetSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus")
	pGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	pIsUserAnAdmin        = shell32.NewProc("IsUserAnAdmin")
	pShellExecute         = shell32.NewProc("ShellExecuteW")
)

const createNoWindow = 0x08000000

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func systemExe(rel string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, rel)
}

func hiddenCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

// runPowerShell runs a script with the PowerShell built into Windows. The script is
// passed encoded on the command line, so nothing is written to disk.
func runPowerShell(script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	var out, errb bytes.Buffer
	cmd := hiddenCmd(ctx, systemExe(`System32\WindowsPowerShell\v1.0\powershell.exe`),
		"-NoProfile", "-NonInteractive", "-STA", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil && out.Len() == 0 {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out.Bytes(), nil
}

func runPowercfg(out string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var buf bytes.Buffer
	cmd := hiddenCmd(ctx, systemExe(`System32\powercfg.exe`), "/batteryreport", "/xml", "/output", out)
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(buf.String())
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	if _, err := os.Stat(out); err != nil {
		return errors.New(strings.TrimSpace(buf.String()))
	}
	return nil
}

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func powerStatus() PowerStatus {
	var s systemPowerStatus
	r, _, _ := pGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s)))
	ps := PowerStatus{Percent: -1, SecondsLeft: -1}
	if r == 0 {
		return ps
	}
	ps.Known = s.BatteryFlag != 255
	ps.HasBattery = s.BatteryFlag != 128 && s.BatteryFlag != 255
	ps.ACOnline = s.ACLineStatus == 1
	ps.Charging = s.BatteryFlag&8 != 0
	ps.Saver = s.SystemStatusFlag == 1
	if s.BatteryLifePercent <= 100 {
		ps.Percent = int(s.BatteryLifePercent)
	}
	if s.BatteryLifeTime != 0xFFFFFFFF {
		ps.SecondsLeft = int64(s.BatteryLifeTime)
	}
	if s.BatteryFlag == 255 && ps.Percent >= 0 {
		ps.HasBattery = true
	}
	return ps
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func isAdmin() bool {
	r, _, _ := pIsUserAnAdmin.Call()
	return r != 0
}

func elevate() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	r, _, _ := pShellExecute.Call(0, uintptr(unsafe.Pointer(u16("runas"))), uintptr(unsafe.Pointer(u16(exe))), 0, 0, 1)
	if r <= 32 {
		return errors.New("Administrator access was not granted")
	}
	return nil
}

func browserPath() string {
	for _, c := range [][2]string{
		{"ProgramFiles(x86)", `Microsoft\Edge\Application\msedge.exe`},
		{"ProgramFiles", `Microsoft\Edge\Application\msedge.exe`},
		{"LOCALAPPDATA", `Microsoft\Edge\Application\msedge.exe`},
		{"ProgramFiles", `Google\Chrome\Application\chrome.exe`},
		{"ProgramFiles(x86)", `Google\Chrome\Application\chrome.exe`},
		{"LOCALAPPDATA", `Google\Chrome\Application\chrome.exe`},
		{"ProgramFiles", `BraveSoftware\Brave-Browser\Application\brave.exe`},
	} {
		if base := os.Getenv(c[0]); base != "" {
			p := filepath.Join(base, c[1])
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// openWindow shows the interface in a chromeless app window. Edge ships with Windows 10 and 11.
func openWindow(url string) {
	if b := browserPath(); b != "" {
		profile := filepath.Join(os.Getenv("LOCALAPPDATA"), "Rigscope", "window")
		cmd := exec.Command(b, "--app="+url, "--user-data-dir="+profile, "--window-size=1240,900",
			"--no-first-run", "--no-default-browser-check", "--disable-extensions", "--disable-sync",
			"--disable-features=Translate,msEdgeSidebarV2,msHubApps,msUndersideButton,msImplicitSignin")
		if cmd.Start() == nil {
			return
		}
	}
	exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}

func showFatal(msg string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(u16(msg))), uintptr(unsafe.Pointer(u16("Rigscope"))), 0x10)
}
