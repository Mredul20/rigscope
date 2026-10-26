//go:build !windows

package main

// Rigscope is a Windows app. This file lets it build and run on Linux and macOS
// with sample data from testdata/, so the interface and logic can be developed
// and tested anywhere. Set RIGSCOPE_SAMPLE to choose a sample (default: laptop).

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func sampleDir() string {
	if d := os.Getenv("RIGSCOPE_TESTDATA"); d != "" {
		return d
	}
	return "testdata"
}

func sampleName() string {
	if s := os.Getenv("RIGSCOPE_SAMPLE"); s != "" {
		return s
	}
	return "laptop"
}

func runPowerShell(script string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(sampleDir(), "specs-"+sampleName()+".json"))
	if err != nil {
		return nil, errors.New("Rigscope reads real hardware on Windows only; no sample data found for " + sampleName())
	}
	return b, nil
}

func runPowercfg(out string) error {
	b, err := os.ReadFile(filepath.Join(sampleDir(), "battery.xml"))
	if err != nil {
		return errors.New("no sample battery report")
	}
	return os.WriteFile(out, b, 0o644)
}

func powerStatus() PowerStatus {
	if sampleName() == "desktop" {
		return PowerStatus{Known: true, Percent: -1, SecondsLeft: -1}
	}
	return PowerStatus{HasBattery: true, Known: true, ACOnline: true, Percent: 86, Charging: true, SecondsLeft: -1}
}

func isAdmin() bool         { return os.Getenv("RIGSCOPE_ADMIN") != "" }
func elevate() error        { return nil }
func showFatal(msg string)  { os.Stderr.WriteString(msg + "\n") }
func openWindow(url string) { exec.Command("xdg-open", url).Start() }
