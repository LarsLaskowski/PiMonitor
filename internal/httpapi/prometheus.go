package httpapi

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/larslaskowski/pimonitor/internal/collector"
)

// prometheusContentType is the Content-Type for the Prometheus text
// exposition format.
// See https://prometheus.io/docs/instrumenting/exposition_formats/.
const prometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

// renderPrometheusMetrics encodes snap as Prometheus text-exposition
// gauges. Every metric is prefixed pimonitor_ and, where the underlying
// data is per-device, labeled the same way GET /api/v1/metrics already
// groups it: core for CPU cores, mount for filesystems, iface for network
// interfaces. The overall CPU figure is its own unlabeled family rather than
// a core="overall" value mixed into the per-core one, so a naive PromQL
// aggregation over the per-core family can't silently double-count it.
// Disks/network are rendered in snap's own order (already filtered of
// pseudo-filesystems/excluded interfaces by the collector), so this stays a
// pure rendering step with no filtering logic of its own.
func renderPrometheusMetrics(snap collector.Snapshot) []byte {
	var buf bytes.Buffer

	writeCPUMetrics(&buf, snap.CPU)
	writeTemperatureMetrics(&buf, snap)
	writeGPUTemperatureMetrics(&buf, snap.GPUTemperature)
	writeMemoryMetrics(&buf, snap.Memory)
	writeSwapMetrics(&buf, snap.Swap)
	writeDiskMetrics(&buf, snap.Disks)
	writeNetworkMetrics(&buf, snap.Network)
	writeUpdatesMetrics(&buf, snap.Updates)

	return buf.Bytes()
}

// writeCPUMetrics writes the overall CPU family and, when present, the
// per-core one. These are deliberately two separate families rather than
// an "overall" value inside the per-core one: mixing an aggregate into the
// series it aggregates makes naive PromQL (sum(...), avg by (...)(...))
// silently double-count unless every query remembers to exclude
// core="overall".
func writeCPUMetrics(buf *bytes.Buffer, cpu collector.CPUUsage) {
	writeGaugeHeader(buf, "pimonitor_cpu_usage_percent", "Overall CPU usage percentage.")
	writeMetric(buf, "pimonitor_cpu_usage_percent", "", "", cpu.OverallPercent)

	if len(cpu.PerCorePercent) == 0 {
		return
	}
	writeGaugeHeader(buf, "pimonitor_cpu_core_usage_percent", "Per-core CPU usage percentage.")
	for i, pct := range cpu.PerCorePercent {
		writeMetric(buf, "pimonitor_cpu_core_usage_percent", "core", strconv.Itoa(i), pct)
	}
}

// writeTemperatureMetrics writes the temperature family only once a
// reading has actually succeeded. Snapshot.Temperature is a plain
// (non-pointer) value that reads as its zero value both before the first
// successful collection and whenever collection fails (e.g. no readable
// thermal zone), and 0°C is a perfectly valid real reading — so unlike the
// JSON API (which keeps its historical documented shape of always
// including the field), rendering it here unconditionally would let a host
// with no sensor report a fabricated {zone=""} 0 sample instead of no
// sample at all, and Prometheus treats an empty label value as the label's
// absence, so a sensor that appears mid-run would silently split the
// metric into two series.
func writeTemperatureMetrics(buf *bytes.Buffer, snap collector.Snapshot) {
	if !snap.TemperatureValid {
		return
	}
	writeGaugeHeader(buf, "pimonitor_temperature_celsius", "CPU temperature in Celsius.")
	writeMetric(buf, "pimonitor_temperature_celsius", "zone", snap.Temperature.Zone, snap.Temperature.Celsius)
}

func writeGPUTemperatureMetrics(buf *bytes.Buffer, gpuTemp *collector.GPUTemperature) {
	if gpuTemp == nil {
		return
	}
	writeGaugeHeader(buf, "pimonitor_gpu_temperature_celsius", "GPU/SoC temperature in Celsius (vcgencmd).")
	writeMetric(buf, "pimonitor_gpu_temperature_celsius", "", "", gpuTemp.Celsius)
}

func writeMemoryMetrics(buf *bytes.Buffer, mem collector.Memory) {
	writeGaugeHeader(buf, "pimonitor_memory_total_bytes", "Total RAM in bytes.")
	writeMetric(buf, "pimonitor_memory_total_bytes", "", "", float64(mem.TotalBytes))
	writeGaugeHeader(buf, "pimonitor_memory_available_bytes", "Available RAM in bytes.")
	writeMetric(buf, "pimonitor_memory_available_bytes", "", "", float64(mem.AvailableBytes))
	writeGaugeHeader(buf, "pimonitor_memory_used_percent", "RAM used percentage.")
	writeMetric(buf, "pimonitor_memory_used_percent", "", "", mem.UsedPercent)
}

func writeSwapMetrics(buf *bytes.Buffer, swap collector.Swap) {
	writeGaugeHeader(buf, "pimonitor_swap_total_bytes", "Total swap in bytes.")
	writeMetric(buf, "pimonitor_swap_total_bytes", "", "", float64(swap.TotalBytes))
	writeGaugeHeader(buf, "pimonitor_swap_used_bytes", "Used swap in bytes.")
	writeMetric(buf, "pimonitor_swap_used_bytes", "", "", float64(swap.UsedBytes))
	writeGaugeHeader(buf, "pimonitor_swap_used_percent", "Swap used percentage.")
	writeMetric(buf, "pimonitor_swap_used_percent", "", "", swap.UsedPercent)
}

func writeDiskMetrics(buf *bytes.Buffer, disks []collector.Disk) {
	if len(disks) == 0 {
		return
	}
	writeGaugeHeader(buf, "pimonitor_disk_total_bytes", "Total filesystem size in bytes.")
	for _, d := range disks {
		writeMetric(buf, "pimonitor_disk_total_bytes", "mount", d.Mountpoint, float64(d.TotalBytes))
	}
	writeGaugeHeader(buf, "pimonitor_disk_used_bytes", "Used filesystem space in bytes.")
	for _, d := range disks {
		writeMetric(buf, "pimonitor_disk_used_bytes", "mount", d.Mountpoint, float64(d.UsedBytes))
	}
	writeGaugeHeader(buf, "pimonitor_disk_used_percent", "Filesystem used percentage (df semantics).")
	for _, d := range disks {
		writeMetric(buf, "pimonitor_disk_used_percent", "mount", d.Mountpoint, d.UsedPercent)
	}
}

func writeNetworkMetrics(buf *bytes.Buffer, ifaces []collector.NetworkInterface) {
	if len(ifaces) == 0 {
		return
	}
	writeGaugeHeader(buf, "pimonitor_network_receive_bytes_per_second", "Network interface receive throughput in bytes/sec.")
	for _, n := range ifaces {
		writeMetric(buf, "pimonitor_network_receive_bytes_per_second", "iface", n.Name, n.RxBytesPerSec)
	}
	writeGaugeHeader(buf, "pimonitor_network_transmit_bytes_per_second", "Network interface transmit throughput in bytes/sec.")
	for _, n := range ifaces {
		writeMetric(buf, "pimonitor_network_transmit_bytes_per_second", "iface", n.Name, n.TxBytesPerSec)
	}
}

func writeUpdatesMetrics(buf *bytes.Buffer, updates collector.Updates) {
	writeGaugeHeader(buf, "pimonitor_updates_pending", "Number of upgradable apt packages.")
	writeMetric(buf, "pimonitor_updates_pending", "", "", float64(updates.Count))
}

// writeGaugeHeader writes the HELP/TYPE comment pair Prometheus's text
// format expects once per metric name, before any of its samples.
func writeGaugeHeader(buf *bytes.Buffer, name, help string) {
	fmt.Fprintf(buf, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
}

// writeMetric writes a single sample line. An empty labelName omits the
// label entirely (for metrics with no per-device dimension) rather than
// emitting an empty label set.
func writeMetric(buf *bytes.Buffer, name, labelName, labelValue string, value float64) {
	if labelName == "" {
		fmt.Fprintf(buf, "%s %s\n", name, formatFloat(value))
		return
	}
	fmt.Fprintf(buf, "%s{%s=\"%s\"} %s\n", name, labelName, escapeLabelValue(labelValue), formatFloat(value))
}

// labelValueReplacer escapes the three characters the Prometheus text
// format requires escaped in a label value; everything else, including
// non-ASCII text, passes through unchanged.
var labelValueReplacer = strings.NewReplacer(`\`, `\\`, "\"", `\"`, "\n", `\n`)

func escapeLabelValue(s string) string {
	return labelValueReplacer.Replace(s)
}

// formatFloat renders a sample value the way the Prometheus text format
// expects: a plain (non-exponential) decimal for finite numbers, and the
// literal NaN/+Inf/-Inf tokens for the rest — which is exactly what
// strconv.FormatFloat already produces for those regardless of format verb.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
