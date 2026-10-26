package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ---- tiny namespace-agnostic XML tree, so small schema differences don't break parsing ----

type xnode struct {
	Name string
	Attr map[string]string
	Text string
	Kids []*xnode
}

func parseXMLTree(b []byte) (*xnode, error) {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE { // UTF-16LE with BOM
		u := make([]uint16, (len(b)-2)/2)
		for i := range u {
			u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
		}
		b = []byte(string(utf16.Decode(u)))
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	root := &xnode{Name: "#root"}
	stack := []*xnode{root}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{Name: strings.ToLower(t.Name.Local), Attr: map[string]string{}}
			for _, a := range t.Attr {
				n.Attr[strings.ToLower(a.Name.Local)] = a.Value
			}
			top := stack[len(stack)-1]
			top.Kids = append(top.Kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			stack[len(stack)-1].Text += string(t)
		}
	}
	return root, nil
}

func (n *xnode) kid(name string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Name == name {
			return k
		}
	}
	return nil
}

func (n *xnode) all(name string) []*xnode {
	if n == nil {
		return nil
	}
	var out []*xnode
	for _, k := range n.Kids {
		if k.Name == name {
			out = append(out, k)
		}
	}
	return out
}

// find returns the first descendant following the path at any depth for the first step.
func (n *xnode) find(path ...string) *xnode {
	if n == nil || len(path) == 0 {
		return n
	}
	if k := n.kid(path[0]); k != nil {
		if r := k.find(path[1:]...); r != nil {
			return r
		}
	}
	for _, k := range n.Kids {
		if r := k.find(path...); r != nil {
			return r
		}
	}
	return nil
}

func (n *xnode) text(name string) string {
	if k := n.kid(name); k != nil {
		return strings.TrimSpace(k.Text)
	}
	return ""
}

func atoi(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		f, err2 := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err2 != nil {
			return 0
		}
		return int64(f)
	}
	return v
}

var isoDur = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:([\d.]+)S)?)?$`)

func parseDuration(s string) int64 {
	m := isoDur.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	var sec float64
	if m[1] != "" {
		sec += float64(atoi(m[1])) * 86400
	}
	if m[2] != "" {
		sec += float64(atoi(m[2])) * 3600
	}
	if m[3] != "" {
		sec += float64(atoi(m[3])) * 60
	}
	if m[4] != "" {
		f, _ := strconv.ParseFloat(m[4], 64)
		sec += f
	}
	return int64(sec)
}

// ---- battery model ----

type HistPoint struct {
	Date   string `json:"date"`
	Full   int64  `json:"full"`
	Design int64  `json:"design"`
}

type UsagePoint struct {
	Time    string  `json:"time"`
	Percent float64 `json:"percent"`
	AC      bool    `json:"ac"`
	Kind    string  `json:"kind"`
}

type PowerStatus struct {
	HasBattery  bool  `json:"hasBattery"`
	Known       bool  `json:"known"`
	ACOnline    bool  `json:"acOnline"`
	Percent     int   `json:"percent"`
	Charging    bool  `json:"charging"`
	SecondsLeft int64 `json:"secondsLeft"`
	Saver       bool  `json:"saver"`
}

type BatteryInfo struct {
	Live         PowerStatus  `json:"live"`
	ReportOK     bool         `json:"reportOK"`
	Error        string       `json:"error,omitempty"`
	Count        int          `json:"count"`
	Name         string       `json:"name"`
	Manufacturer string       `json:"manufacturer"`
	Chemistry    string       `json:"chemistry"`
	Design       int64        `json:"design"`
	Full         int64        `json:"full"`
	Cycles       int64        `json:"cycles"`
	SysMaker     string       `json:"sysMaker"`
	SysModel     string       `json:"sysModel"`
	RunFullSec   int64        `json:"runFullSec"`
	RunDesignSec int64        `json:"runDesignSec"`
	History      []HistPoint  `json:"history"`
	Recent       []UsagePoint `json:"recent"`
}

func parseBatteryReport(b []byte, info *BatteryInfo) error {
	root, err := parseXMLTree(b)
	if err != nil {
		return err
	}
	sys := root.find("systeminformation")
	info.SysMaker = sys.text("systemmanufacturer")
	info.SysModel = sys.text("systemproductname")

	var names, makers []string
	info.Cycles = -1
	for _, bt := range root.find("batteries").all("battery") {
		info.Count++
		info.Design += atoi(bt.text("designcapacity"))
		info.Full += atoi(bt.text("fullchargecapacity"))
		if c := bt.text("cyclecount"); c != "" && atoi(c) > 0 {
			if info.Cycles < 0 {
				info.Cycles = 0
			}
			info.Cycles += atoi(c)
		}
		if id := bt.text("id"); id != "" {
			names = append(names, id)
		}
		if m := bt.text("manufacturer"); m != "" {
			makers = append(makers, m)
		}
		if info.Chemistry == "" {
			info.Chemistry = bt.text("chemistry")
		}
	}
	info.Name = strings.Join(names, ", ")
	info.Manufacturer = strings.Join(makers, ", ")

	if rt := root.find("runtimeestimates"); rt != nil {
		info.RunFullSec = parseDuration(rt.find("fullchargecapacity", "activeruntime").textOrEmpty())
		info.RunDesignSec = parseDuration(rt.find("designcapacity", "activeruntime").textOrEmpty())
	}

	for _, h := range root.find("history").all("historyentry") {
		full, design := atoi(h.Attr["fullchargecapacity"]), atoi(h.Attr["designcapacity"])
		date := h.Attr["localstartdate"]
		if date == "" {
			date = h.Attr["startdate"]
		}
		if full > 0 && design > 0 && date != "" {
			info.History = append(info.History, HistPoint{Date: date, Full: full, Design: design})
		}
	}
	sort.SliceStable(info.History, func(i, j int) bool { return info.History[i].Date < info.History[j].Date })

	for _, u := range root.find("recentusage").all("usageentry") {
		charge, full := atoi(u.Attr["chargecapacity"]), atoi(u.Attr["fullchargecapacity"])
		ts := u.Attr["localtimestamp"]
		if ts == "" {
			ts = u.Attr["timestamp"]
		}
		if full <= 0 || ts == "" {
			continue
		}
		pct := float64(charge) / float64(full) * 100
		if pct > 100 {
			pct = 100
		}
		info.Recent = append(info.Recent, UsagePoint{Time: ts, Percent: pct, AC: u.Attr["ac"] == "1", Kind: u.Attr["entrytype"]})
	}
	if info.History == nil {
		info.History = []HistPoint{}
	}
	if info.Recent == nil {
		info.Recent = []UsagePoint{}
	}
	info.ReportOK = info.Count > 0 && info.Design > 0
	return nil
}

func (n *xnode) textOrEmpty() string {
	if n == nil {
		return ""
	}
	return strings.TrimSpace(n.Text)
}

func readBattery() BatteryInfo {
	info := BatteryInfo{Live: powerStatus(), History: []HistPoint{}, Recent: []UsagePoint{}, Cycles: -1}
	if !info.Live.HasBattery {
		return info
	}
	out := filepath.Join(os.TempDir(), "rigscope-battery-report.xml")
	os.Remove(out)
	if err := runPowercfg(out); err != nil {
		info.Error = "Windows couldn't create a battery report: " + err.Error()
		return info
	}
	b, err := os.ReadFile(out)
	os.Remove(out)
	if err != nil {
		info.Error = "The battery report couldn't be read."
		return info
	}
	if err := parseBatteryReport(b, &info); err != nil {
		info.Error = "The battery report couldn't be understood: " + err.Error()
	}
	return info
}
