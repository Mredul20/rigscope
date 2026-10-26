package main

import (
	"fmt"
	"strings"
	"time"
)

type Check struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // good, warn, bad, info, unknown
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

type SecReport struct {
	Good    int     `json:"good"`
	Graded  int     `json:"graded"`
	Checks  []Check `json:"checks"`
	Win11   []Check `json:"win11"`
	Win11OK string  `json:"win11Ok"` // yes, no, unknown
	raw     rawSecurity
}

func day(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func human(t time.Time) string { return t.Format("2 January 2006") }

// Security Center's productState packs flags: bits 12-15 of the middle byte say on/off, the low byte says outdated.
func avOn(state int64) bool       { return (state>>8)&0x10 != 0 }
func avOutdated(state int64) bool { return state&0x10 != 0 }

func editionClass(ed string) string {
	e := strings.ToLower(ed)
	switch {
	case strings.Contains(e, "enterprises"), strings.Contains(e, "iotenterprises"):
		return "ltsc"
	case strings.Contains(e, "enterprise"), strings.Contains(e, "education"):
		return "ent"
	}
	return "home"
}

func windowsSupport(o rawOS, now time.Time) Check {
	c := Check{ID: "windows", Title: "Windows security updates", Status: "unknown"}
	w := catalog.Windows
	cls := editionClass(o.Edition)
	switch {
	case o.Build == 0:
		c.Detail = "Couldn't read the Windows version."
		return c
	case o.Build < 22000:
		if cls == "ltsc" {
			for _, v := range w.Win10LTS {
				if v.Build == o.Build {
					end := day(v.HomeEnd)
					if now.Before(end) {
						c.Status, c.Detail = "good", fmt.Sprintf("Windows 10 Enterprise %s is supported until %s.", v.Name, human(end))
					} else {
						c.Status, c.Detail = "bad", fmt.Sprintf("Windows 10 Enterprise %s stopped getting updates on %s.", v.Name, human(end))
					}
					return c
				}
			}
		}
		end, esu := day(w.Win10.HomeEnd), day(w.Win10ESU)
		if now.Before(end) {
			c.Status, c.Detail = "good", "Windows 10 is supported until "+human(end)+"."
			return c
		}
		if o.Build != w.Win10.Build {
			c.Status = "bad"
			c.Detail = "This Windows 10 version no longer gets any security updates."
			c.Fix = "Upgrade to Windows 11 if this PC supports it, or at least update to Windows 10 version 22H2 so you can join Extended Security Updates."
			return c
		}
		c.Status = "warn"
		c.Detail = fmt.Sprintf("Windows 10 stopped getting free security updates on %s. If you joined Extended Security Updates, you're covered until %s.", human(end), human(esu))
		c.Fix = "Upgrade to Windows 11 if the check below says this PC can. If not, join Extended Security Updates from Settings, Windows Update."
		if !now.Before(esu) {
			c.Status = "bad"
			c.Detail = "Windows 10 no longer gets security updates, including Extended Security Updates."
		}
		return c
	}
	var match *WinVersion
	for i, v := range w.Win11 {
		if v.Build == o.Build {
			match = &w.Win11[i]
		}
	}
	if match == nil {
		c.Detail = fmt.Sprintf("Rigscope doesn't know build %d. It may be a Windows Insider preview, or a version released after this copy of Rigscope.", o.Build)
		return c
	}
	endS := match.HomeEnd
	who := "Home and Pro"
	if cls == "ent" {
		endS, who = match.EntEnd, "Enterprise and Education"
	}
	if cls == "ltsc" && match.LTSCEnd != "" {
		endS, who = match.LTSCEnd, "LTSC"
	}
	end := day(endS)
	if now.Before(end) {
		c.Status = "good"
		c.Detail = fmt.Sprintf("Windows 11 %s (%s) gets security updates until %s.", match.Name, who, human(end))
		if end.Sub(now) < 75*24*time.Hour {
			c.Status = "warn"
			c.Fix = "Support ends soon. Install the newest Windows 11 version from Settings, Windows Update."
		}
		return c
	}
	c.Status = "bad"
	c.Detail = fmt.Sprintf("Windows 11 %s (%s) stopped getting security updates on %s.", match.Name, who, human(end))
	c.Fix = "Open Settings, Windows Update and install the newest Windows 11 version. It's a free update and keeps your files and apps."
	return c
}

func evaluateSecurity(raw *rawSpecs, r *Report, now time.Time) SecReport {
	s := raw.Security
	rep := SecReport{raw: s, Checks: []Check{}, Win11: []Check{}}
	add := func(c Check) { rep.Checks = append(rep.Checks, c) }

	// antivirus
	av := Check{ID: "antivirus", Title: "Antivirus", Status: "unknown", Detail: "Windows didn't report an antivirus status."}
	var on, outdated []string
	for _, a := range s.AV {
		if avOn(a.State) {
			if avOutdated(a.State) {
				outdated = append(outdated, a.Name)
			} else {
				on = append(on, a.Name)
			}
		}
	}
	switch {
	case len(on) > 0:
		av.Status, av.Detail = "good", joinAnd(on)+" is on and up to date."
	case len(outdated) > 0:
		av.Status, av.Detail = "warn", joinAnd(outdated)+" is on, but its virus definitions are out of date."
		av.Fix = "Open the antivirus and run an update, or check that this PC is connected to the internet."
	case len(s.AV) > 0:
		av.Status, av.Detail = "bad", "No antivirus is actively protecting this PC."
		av.Fix = "Open Windows Security, Virus & threat protection and turn on Real-time protection."
	case s.Defender != nil && s.Defender.Enabled && s.Defender.Realtime:
		av.Status, av.Detail = "good", "Microsoft Defender Antivirus is on."
	case s.Defender != nil:
		av.Status, av.Detail = "bad", "Microsoft Defender's real-time protection is off."
		av.Fix = "Open Windows Security, Virus & threat protection and turn on Real-time protection."
	}
	if av.Status == "good" && s.Defender != nil && s.Defender.Mode == "Normal" && s.Defender.SigAge > 7 {
		av.Status = "warn"
		av.Detail = fmt.Sprintf("Microsoft Defender is on, but its virus definitions are %d days old.", s.Defender.SigAge)
		av.Fix = "Open Windows Security, Virus & threat protection and click Check for updates."
	}
	add(av)

	// firewall
	fw := Check{ID: "firewall", Title: "Firewall", Status: "unknown", Detail: "Windows didn't report the firewall status."}
	var off []string
	for _, p := range s.Firewall {
		if !p.Enabled {
			off = append(off, strings.ToLower(p.Name))
		}
	}
	other := ""
	for _, f := range s.FW3P {
		if avOn(f.State) {
			other = f.Name
		}
	}
	switch {
	case len(s.Firewall) == 0 && other != "":
		fw.Status, fw.Detail = "good", other+" is protecting this PC."
	case len(s.Firewall) == 0:
	case len(off) == 0:
		fw.Status, fw.Detail = "good", "Windows Firewall is on for every network type."
	case other != "":
		fw.Status, fw.Detail = "good", other+" is on. Windows Firewall is off for "+joinAnd(off)+" networks, which is normal when another firewall is installed."
	default:
		fw.Status = "warn"
		if contains(off, "public") {
			fw.Status = "bad"
		}
		fw.Detail = "Windows Firewall is off for " + joinAnd(off) + " networks."
		fw.Fix = "Open Windows Security, Firewall & network protection and turn the firewall on for each network."
	}
	add(fw)

	// Windows support and updates
	add(windowsSupport(raw.OS, now))
	up := Check{ID: "updates", Title: "Latest updates installed", Status: "unknown", Detail: "Couldn't tell when the last update was installed."}
	latest := 0
	for _, v := range catalog.Windows.Win11 {
		if v.Build == raw.OS.Build {
			latest = v.LatestUBR
		}
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s.LastUpdate); err == nil {
		days := int(now.Sub(t).Hours() / 24)
		when := fmt.Sprintf("%d days ago", days)
		if days <= 1 {
			when = "in the last day"
		}
		up.Detail = "The last update (" + s.LastUpdateID + ") was installed " + when + "."
		switch {
		case latest > 0 && raw.OS.UBR >= latest:
			up.Status = "good"
			up.Detail += fmt.Sprintf(" Windows is on build %d.%d, the newest Rigscope knows of.", raw.OS.Build, raw.OS.UBR)
		case days <= 45:
			up.Status = "good"
		case days <= 90:
			up.Status, up.Fix = "warn", "Open Settings, Windows Update and click Check for updates."
		default:
			up.Status, up.Fix = "bad", "Updates seem to be stuck or paused. Open Settings, Windows Update, resume updates and install everything offered."
		}
	}
	add(up)

	// hardware security
	sb := Check{ID: "secureboot", Title: "Secure Boot", Detail: ""}
	legacy := strings.EqualFold(raw.System.Firmware, "Legacy")
	switch {
	case s.SecureBoot == 1:
		sb.Status, sb.Detail = "good", "Only trusted software can load while the PC starts up."
	case legacy:
		sb.Status, sb.Detail = "warn", "This PC starts in old BIOS (legacy) mode, which can't use Secure Boot."
		sb.Fix = "Switching to UEFI mode needs the drive converted to GPT first (Windows has a tool called MBR2GPT). Ask someone experienced if you're unsure."
	case s.SecureBoot == 0:
		sb.Status, sb.Detail = "warn", "Secure Boot is supported but turned off. Some games, like Valorant and Battlefield 6, won't start without it."
		sb.Fix = "Restart into the BIOS or UEFI settings (usually F2, F10 or Delete at startup), find Secure Boot under Boot or Security, and turn it on."
	default:
		sb.Status, sb.Detail = "unknown", "Couldn't read the Secure Boot setting."
	}
	add(sb)

	tpm := Check{ID: "tpm", Title: "TPM 2.0 security chip"}
	is2 := strings.Contains(s.TPMName, "2.0") || strings.HasPrefix(s.TPMSpec, "2.0")
	switch {
	case s.TPMOK && is2:
		tpm.Status, tpm.Detail = "good", "TPM 2.0 is on. It protects encryption keys and your sign-in."
	case s.TPMOK:
		tpm.Status, tpm.Detail = "warn", "This PC has an older TPM ("+s.TPMName+"). Windows 11 needs version 2.0."
	default:
		tpm.Status, tpm.Detail = "warn", "No TPM was found. Most PCs from 2017 on have one built in, but it may be turned off."
		tpm.Fix = "In the BIOS or UEFI settings, look for Intel PTT, AMD fTPM or Security Device Support and turn it on."
	}
	add(tpm)

	// encryption
	enc := Check{ID: "encryption", Title: "Drive encryption", Status: "unknown"}
	sys := "C:"
	for _, v := range s.BitLocker {
		if strings.EqualFold(v.Letter, sys) {
			switch v.State {
			case 1, 6:
				enc.Status, enc.Detail = "good", "BitLocker protects drive C:. If the PC is lost, nobody can read your files without signing in."
			case 3, 4:
				enc.Status, enc.Detail = "info", "BitLocker is encrypting or decrypting drive C: right now."
			case 5:
				enc.Status, enc.Detail = "warn", "BitLocker is suspended on drive C:, so the drive isn't protected right now."
				enc.Fix = "Open Control Panel, BitLocker Drive Encryption and click Resume protection."
			case 8:
				enc.Status, enc.Detail = "warn", "Drive C: is encrypted, but protection isn't turned on yet."
				enc.Fix = "Sign in to Windows with a Microsoft account, or open Settings, Privacy & security, Device encryption to finish turning it on."
			case 2:
				enc.Detail = "Drive C: isn't encrypted. Anyone with the drive can read your files."
				if r.Laptop {
					enc.Status = "warn"
				} else {
					enc.Status = "info"
				}
				if editionClass(raw.OS.Edition) == "home" && strings.Contains(strings.ToLower(raw.OS.Edition), "core") {
					enc.Fix = "Windows Home offers Device encryption on some PCs: check Settings, Privacy & security, Device encryption. BitLocker needs Windows Pro."
				} else {
					enc.Fix = "Search for Manage BitLocker in the Start menu and turn it on for drive C:. Save the recovery key somewhere safe."
				}
			default:
				enc.Detail = "This drive doesn't support encryption, or Windows didn't report its status."
			}
		}
	}
	if enc.Detail == "" {
		enc.Detail = "Couldn't read the encryption status."
	}
	add(enc)

	uac := Check{ID: "uac", Title: "User Account Control"}
	switch {
	case s.UAC == 0:
		uac.Status, uac.Detail = "bad", "User Account Control is turned off, so any program can change system settings without asking."
		uac.Fix = "Search for Change User Account Control settings in the Start menu, move the slider to the default level and restart."
	case s.UACPrompt == 0:
		uac.Status, uac.Detail = "warn", "User Account Control is set to never notify."
		uac.Fix = "Search for Change User Account Control settings in the Start menu and move the slider back to the default level."
	case s.UAC == 1:
		uac.Status, uac.Detail = "good", "User Account Control asks before apps change system settings."
	default:
		uac.Status, uac.Detail = "unknown", "Couldn't read the User Account Control setting."
	}
	add(uac)

	mi := Check{ID: "memoryintegrity", Title: "Memory integrity"}
	switch s.MemoryIntegrity {
	case 1:
		mi.Status, mi.Detail = "good", "Core isolation blocks malicious drivers from taking over Windows."
	default:
		mi.Status, mi.Detail = "warn", "Core isolation memory integrity is off. It blocks malicious drivers, but very old drivers can stop it from turning on."
		mi.Fix = "Open Windows Security, Device security, Core isolation details and turn on Memory integrity."
	}
	add(mi)

	ss := Check{ID: "smartscreen", Title: "SmartScreen download checks"}
	if strings.EqualFold(s.SmartScreen, "off") || s.SmartScreenPolicy == 0 {
		ss.Status, ss.Detail = "warn", "SmartScreen is off, so Windows won't warn you before you run unknown downloaded apps."
		ss.Fix = "Open Windows Security, App & browser control, Reputation-based protection settings and turn on Check apps and files."
	} else {
		ss.Status, ss.Detail = "good", "SmartScreen warns you before running apps that Windows doesn't recognize."
	}
	add(ss)

	for _, c := range rep.Checks {
		switch c.Status {
		case "good":
			rep.Good++
			rep.Graded++
		case "warn", "bad":
			rep.Graded++
		}
	}

	// Windows 11 hardware requirements
	w := []Check{}
	cpuC := Check{ID: "w11cpu", Title: "Processor is on Microsoft's supported list"}
	switch r.CPU.Win11 {
	case "yes":
		cpuC.Status, cpuC.Detail = "good", r.CPU.Family+" processors are supported."
	case "no":
		cpuC.Status, cpuC.Detail = "bad", r.CPU.Family+" processors aren't on the list. Windows 11 needs Intel 8th gen, AMD Ryzen 2000 or newer."
	default:
		cpuC.Status, cpuC.Detail = "unknown", "Rigscope can't tell for this processor. Check Microsoft's PC Health Check app."
	}
	w = append(w, cpuC)
	tc := Check{ID: "w11tpm", Title: "TPM 2.0"}
	if s.TPMOK && is2 {
		tc.Status, tc.Detail = "good", "Found and turned on."
	} else {
		tc.Status, tc.Detail = "bad", "Not found or turned off. It may only need turning on in the BIOS."
	}
	w = append(w, tc)
	ub := Check{ID: "w11uefi", Title: "UEFI with Secure Boot support"}
	switch {
	case s.SecureBoot >= 0 || strings.EqualFold(raw.System.Firmware, "UEFI"):
		ub.Status, ub.Detail = "good", "This PC starts in UEFI mode."
	case legacy:
		ub.Status, ub.Detail = "bad", "This PC starts in legacy BIOS mode."
	default:
		ub.Status, ub.Detail = "unknown", "Couldn't tell how this PC starts."
	}
	w = append(w, ub)
	rc := Check{ID: "w11ram", Title: "4 GB memory or more"}
	if ramGB(r.Memory.Total) >= 4 {
		rc.Status, rc.Detail = "good", fmt.Sprintf("%d GB installed.", ramGB(r.Memory.Total))
	} else {
		rc.Status, rc.Detail = "bad", fmt.Sprintf("Only %d GB installed.", ramGB(r.Memory.Total))
	}
	w = append(w, rc)
	dc := Check{ID: "w11disk", Title: "64 GB storage or more", Status: "unknown", Detail: "Couldn't read the drive size."}
	for _, v := range r.Volumes {
		if strings.EqualFold(v.Letter, sys) {
			if v.Size >= 60<<30 {
				dc.Status, dc.Detail = "good", fmt.Sprintf("Drive C: is %d GB.", v.Size>>30)
			} else {
				dc.Status, dc.Detail = "bad", fmt.Sprintf("Drive C: is only %d GB.", v.Size>>30)
			}
		}
	}
	w = append(w, dc)
	rep.Win11 = w
	rep.Win11OK = "yes"
	for _, c := range w {
		if c.Status == "bad" {
			rep.Win11OK = "no"
			break
		}
		if c.Status == "unknown" {
			rep.Win11OK = "unknown"
		}
	}
	return rep
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
