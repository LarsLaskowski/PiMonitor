package httpapi

import (
	"math"
	"strings"
	"testing"

	"github.com/larslaskowski/pimonitor/internal/collector"
)

// fullSnapshot exercises every field renderPrometheusMetrics knows about,
// including the optional/per-device ones (GPU temperature, multiple CPU
// cores, disks, network interfaces).
func fullSnapshot() collector.Snapshot {
	return collector.Snapshot{
		CPU: collector.CPUUsage{
			OverallPercent: 12.5,
			PerCorePercent: []float64{10, 15},
		},
		Temperature:      collector.Temperature{Zone: "cpu-thermal", Celsius: 48.6},
		TemperatureValid: true,
		GPUTemperature:   &collector.GPUTemperature{Celsius: 47.8},
		Memory:           collector.Memory{TotalBytes: 4137000000, AvailableBytes: 2900000000, UsedPercent: 29.9},
		Swap:             collector.Swap{TotalBytes: 104857600, UsedBytes: 0, UsedPercent: 0},
		Disks: []collector.Disk{
			{Mountpoint: "/", TotalBytes: 31000000000, UsedBytes: 8000000000, UsedPercent: 25.8},
		},
		Network: []collector.NetworkInterface{
			{Name: "eth0", RxBytesPerSec: 1240.5, TxBytesPerSec: 302.1},
		},
		Updates: collector.Updates{Count: 3},
	}
}

func TestRenderPrometheusMetrics_LabelsAndValues(t *testing.T) {
	body := string(renderPrometheusMetrics(fullSnapshot()))

	tests := []struct {
		name string
		want string
	}{
		{"overall CPU gauge, unlabeled", "pimonitor_cpu_usage_percent 12.5"},
		{"per-core CPU gauge, core 0", `pimonitor_cpu_core_usage_percent{core="0"} 10`},
		{"per-core CPU gauge, core 1", `pimonitor_cpu_core_usage_percent{core="1"} 15`},
		{"temperature gauge with zone label", `pimonitor_temperature_celsius{zone="cpu-thermal"} 48.6`},
		{"GPU temperature gauge", "pimonitor_gpu_temperature_celsius 47.8"},
		{"memory total bytes", "pimonitor_memory_total_bytes 4137000000"},
		{"memory available bytes", "pimonitor_memory_available_bytes 2900000000"},
		{"memory used percent", "pimonitor_memory_used_percent 29.9"},
		{"swap total bytes", "pimonitor_swap_total_bytes 104857600"},
		{"disk total bytes with mount label", `pimonitor_disk_total_bytes{mount="/"} 31000000000`},
		{"disk used bytes with mount label", `pimonitor_disk_used_bytes{mount="/"} 8000000000`},
		{"disk used percent with mount label", `pimonitor_disk_used_percent{mount="/"} 25.8`},
		{"network receive gauge with iface label", `pimonitor_network_receive_bytes_per_second{iface="eth0"} 1240.5`},
		{"network transmit gauge with iface label", `pimonitor_network_transmit_bytes_per_second{iface="eth0"} 302.1`},
		{"updates pending gauge", "pimonitor_updates_pending 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(body, tt.want) {
				t.Fatalf("output missing line %q\nfull output:\n%s", tt.want, body)
			}
		})
	}
}

// TestRenderPrometheusMetrics_HelpAndTypeComments pins the exposition
// format's required HELP/TYPE comment pair, which promtool checks for
// every metric family.
func TestRenderPrometheusMetrics_HelpAndTypeComments(t *testing.T) {
	body := string(renderPrometheusMetrics(fullSnapshot()))

	for _, name := range []string{
		"pimonitor_cpu_usage_percent",
		"pimonitor_cpu_core_usage_percent",
		"pimonitor_temperature_celsius",
		"pimonitor_memory_used_percent",
		"pimonitor_disk_used_percent",
		"pimonitor_network_receive_bytes_per_second",
		"pimonitor_updates_pending",
	} {
		if !strings.Contains(body, "# HELP "+name+" ") {
			t.Errorf("missing HELP comment for %s", name)
		}
		if !strings.Contains(body, "# TYPE "+name+" gauge") {
			t.Errorf("missing TYPE comment for %s", name)
		}
	}
}

// TestRenderPrometheusMetrics_OmitsAbsentOptionalFields guards the same
// omit-when-absent behavior GET /api/v1/metrics already documents: no GPU
// temperature without vcgencmd, no network section when monitoring is
// disabled (empty slice), no disk section without any mounted filesystem,
// and no per-core CPU family without per-core data.
func TestRenderPrometheusMetrics_OmitsAbsentOptionalFields(t *testing.T) {
	snap := collector.Snapshot{
		CPU:              collector.CPUUsage{OverallPercent: 5},
		Temperature:      collector.Temperature{Zone: "cpu-thermal", Celsius: 40},
		TemperatureValid: true,
	}

	body := string(renderPrometheusMetrics(snap))

	for _, absent := range []string{
		"pimonitor_cpu_core_usage_percent",
		"pimonitor_gpu_temperature_celsius",
		"pimonitor_disk_",
		"pimonitor_network_",
	} {
		if strings.Contains(body, absent) {
			t.Fatalf("expected no %s* lines when the field is absent/empty, got:\n%s", absent, body)
		}
	}
}

// TestRenderPrometheusMetrics_OmitsTemperatureWhenInvalid is the regression
// test for issue #146's review: without a successful temperature
// collection, the family must be skipped entirely rather than rendering a
// fabricated {zone=""} 0 sample — a real 0°C reading is indistinguishable
// from "no sensor" once encoded that way, and Prometheus treats an empty
// label value as the label's absence, so a sensor that appears later would
// silently split the metric into two series.
func TestRenderPrometheusMetrics_OmitsTemperatureWhenInvalid(t *testing.T) {
	// TemperatureValid left at its zero value (false), as it is before the
	// first successful collection or after a failed one. Zone/Celsius are
	// deliberately populated with what looks like a real reading, to pin
	// that the guard is TemperatureValid itself, not an empty-Zone check
	// (findCPUThermalZone can leave Zone empty on a genuine reading too).
	snap := collector.Snapshot{
		CPU:         collector.CPUUsage{OverallPercent: 5},
		Temperature: collector.Temperature{Zone: "cpu-thermal", Celsius: 40},
	}

	body := string(renderPrometheusMetrics(snap))

	if strings.Contains(body, "pimonitor_temperature_celsius") {
		t.Fatalf("expected no pimonitor_temperature_celsius line when TemperatureValid is false, got:\n%s", body)
	}
}

// TestEscapeLabelValue guards the three escapes the Prometheus text format
// requires for label values; a mountpoint or interface name containing a
// backslash, quote, or newline must not break the exposition format's
// quoting.
func TestEscapeLabelValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain value", "/data", "/data"},
		{"backslash", `C:\data`, `C:\\data`},
		{"double quote", `weird"mount`, `weird\"mount`},
		{"newline", "line1\nline2", `line1\nline2`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeLabelValue(tt.input); got != tt.want {
				t.Fatalf("escapeLabelValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestFormatFloat pins the non-exponential formatting for ordinary values
// and the literal tokens the Prometheus text format requires for
// non-finite ones.
func TestFormatFloat(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  string
	}{
		{"integer-valued float", 4137000000, "4137000000"},
		{"fractional value", 12.5, "12.5"},
		{"zero", 0, "0"},
		{"NaN", math.NaN(), "NaN"},
		{"positive infinity", math.Inf(1), "+Inf"},
		{"negative infinity", math.Inf(-1), "-Inf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatFloat(tt.input); got != tt.want {
				t.Fatalf("formatFloat(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
