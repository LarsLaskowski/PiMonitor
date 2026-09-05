// Package collector reads system metrics from /proc, /sys, and apt, and
// exposes them as a periodically refreshed Snapshot with short in-memory
// history.
package collector

import "time"

// CPUUsage is the instantaneous CPU utilization, derived from deltas
// between two /proc/stat samples.
type CPUUsage struct {
	OverallPercent float64   `json:"overall_percent"`
	PerCorePercent []float64 `json:"per_core_percent,omitempty"`
}

// CPUCoreFrequency is a single CPU core's current clock speed and active
// scaling governor, read from sysfs cpufreq.
type CPUCoreFrequency struct {
	Core     int     `json:"core"`
	MHz      float64 `json:"mhz"`
	Governor string  `json:"governor"`
}

// LoadAverage is the standard Unix load average as reported by the kernel.
type LoadAverage struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// Temperature is a single thermal-zone reading.
type Temperature struct {
	Zone    string  `json:"zone"`
	Celsius float64 `json:"celsius"`
}

// GPUTemperature is the optional vcgencmd-sourced GPU/SoC temperature.
type GPUTemperature struct {
	Celsius float64 `json:"celsius"`
}

// Throttled is the Raspberry Pi under-voltage / throttling state decoded
// from the `vcgencmd get_throttled` bitmask. The *Now flags reflect the
// current state; the *SinceBoot flags latch whether the condition has
// occurred at any point since boot.
type Throttled struct {
	UnderVoltageNow          bool `json:"under_voltage_now"`
	FrequencyCappedNow       bool `json:"frequency_capped_now"`
	ThrottledNow             bool `json:"throttled_now"`
	SoftTempLimitNow         bool `json:"soft_temp_limit_now"`
	UnderVoltageSinceBoot    bool `json:"under_voltage_since_boot"`
	FrequencyCappedSinceBoot bool `json:"frequency_capped_since_boot"`
	ThrottledSinceBoot       bool `json:"throttled_since_boot"`
	SoftTempLimitSinceBoot   bool `json:"soft_temp_limit_since_boot"`
	// Raw is the original hex bitmask string (e.g. "0x50005").
	Raw string `json:"raw"`
}

// Memory holds RAM usage figures.
type Memory struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

// Swap holds swap usage figures.
type Swap struct {
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// Disk is the usage of a single mounted filesystem.
type Disk struct {
	Mountpoint  string  `json:"mountpoint"`
	Device      string  `json:"device"`
	FSType      string  `json:"fstype"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// NetworkInterface is the throughput of a single network interface,
// computed from a delta between two /proc/net/dev samples.
type NetworkInterface struct {
	Name          string  `json:"name"`
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
}

// DiskIO is the read/write throughput of a single block device, computed
// from a delta between two /proc/diskstats samples (sectors × 512 bytes).
type DiskIO struct {
	Device           string  `json:"device"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
}

// SystemInfo holds identity information that rarely changes at runtime.
type SystemInfo struct {
	KernelVersion string `json:"kernel_version"`
	Distribution  string `json:"distribution"`
	PiModel       string `json:"pi_model"`
	CPUModel      string `json:"cpu_model"`
}

// PackageUpdate is a single upgradable apt package.
type PackageUpdate struct {
	Name       string `json:"name"`
	NewVersion string `json:"new_version"`
	OldVersion string `json:"old_version,omitempty"`
	Arch       string `json:"arch,omitempty"`
}

// Updates summarizes available apt package updates and the freshness of
// the underlying apt cache (refreshed out-of-band by a root-privileged
// systemd timer; this process only ever reads it).
type Updates struct {
	Count           int             `json:"count"`
	Packages        []PackageUpdate `json:"packages,omitempty"`
	CacheAgeSeconds float64         `json:"cache_age_seconds"`
	Stale           bool            `json:"stale"`
	CheckedAt       time.Time       `json:"checked_at"`
}

// Snapshot is the full set of current metric values.
type Snapshot struct {
	Timestamp     time.Time          `json:"timestamp"`
	UptimeSeconds float64            `json:"uptime_seconds"`
	CPU           CPUUsage           `json:"cpu"`
	CPUFrequency  []CPUCoreFrequency `json:"cpu_frequency,omitempty"`
	Load          LoadAverage        `json:"load_average"`
	CPUCount      int                `json:"cpu_count"`
	Temperature   Temperature        `json:"temperature"`
	// TemperatureValid reports whether the most recent temperature
	// collection succeeded — false both before the first successful
	// collection and whenever the most recent one failed (e.g. no readable
	// thermal zone). Temperature itself stays a plain, non-pointer value so
	// the /api/v1/metrics JSON shape (which has always reported zero values
	// there rather than omitting the field) doesn't change; this field is
	// deliberately excluded from JSON via json:"-" and exists purely for
	// in-process consumers — namely the Prometheus renderer in
	// internal/httpapi — that need to tell a genuine 0°C reading apart from
	// no reading at all, the same distinction alert.Sample.TemperatureValid
	// already draws for the alert engine.
	TemperatureValid bool               `json:"-"`
	GPUTemperature   *GPUTemperature    `json:"gpu_temperature,omitempty"`
	Throttled        *Throttled         `json:"throttled,omitempty"`
	Memory           Memory             `json:"memory"`
	Swap             Swap               `json:"swap"`
	Disks            []Disk             `json:"disks"`
	DiskIO           []DiskIO           `json:"disk_io"`
	Network          []NetworkInterface `json:"network,omitempty"`
	System           SystemInfo         `json:"system"`
	Updates          Updates            `json:"updates"`
}

// HistoryPoint is a single timestamped sample in a metric's ring buffer.
type HistoryPoint struct {
	Timestamp time.Time `json:"t"`
	Value     float64   `json:"v"`
}
