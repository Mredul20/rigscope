package main

// Disk is one physical drive, as reported by the Windows Storage Management API.
// Wear, temperature, power-on hours and error counts are nil unless Rigscope runs as administrator.
type Disk struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Model       string   `json:"model"`
	Firmware    string   `json:"firmware"`
	Media       int      `json:"media"`
	Bus         int      `json:"bus"`
	Size        int64    `json:"size"`
	Health      int      `json:"health"`
	Spindle     int64    `json:"spindle"`
	Letters     []string `json:"letters"`
	Rel         bool     `json:"rel"`
	RelErr      string   `json:"relErr"`
	Wear        *float64 `json:"wear"`
	Temp        *float64 `json:"temp"`
	TempMax     *float64 `json:"tempMax"`
	Hours       *float64 `json:"hours"`
	ReadErr     *float64 `json:"readErr"`
	ReadErrUnc  *float64 `json:"readErrUnc"`
	WriteErr    *float64 `json:"writeErr"`
	WriteErrUnc *float64 `json:"writeErrUnc"`
	StartStop   *float64 `json:"startStop"`

	Kind    string `json:"kind"`    // SSD, HDD or Drive
	BusName string `json:"busName"` // NVMe, SATA, USB...
	State   string `json:"state"`   // healthy, warning, unhealthy, unknown
}

var busNames = map[int]string{1: "SCSI", 3: "ATA", 6: "Fibre Channel", 7: "USB", 8: "RAID", 9: "iSCSI", 10: "SAS",
	11: "SATA", 12: "SD card", 13: "MMC", 15: "Virtual", 16: "Storage Spaces", 17: "NVMe", 18: "UFS"}

func finishDisk(d *Disk) {
	if d.Letters == nil {
		d.Letters = []string{}
	}
	d.BusName = busNames[d.Bus]
	switch {
	case d.Media == 4 || d.Bus == 17 || d.Bus == 18:
		d.Kind = "SSD"
	case d.Media == 3 || (d.Spindle > 0 && d.Spindle < 100000):
		d.Kind = "HDD"
	default:
		d.Kind = "Drive"
	}
	switch d.Health {
	case 0:
		d.State = "healthy"
	case 1:
		d.State = "warning"
	case 2:
		d.State = "unhealthy"
	default:
		d.State = "unknown"
	}
}
