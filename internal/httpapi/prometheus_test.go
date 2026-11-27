package httpapi

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/larslaskowski/pimonitor/internal/collector"
)

// fullSnapshot exercises every field renderPrometheusMetrics knows about,
// including the optional/per-device ones (GPU and PMIC temperature, multiple CPU
// cores, disks, network interfaces).
func fullSnapshot() collector.Snapshot {
	return collector.Snapshot{
		CPU: collector.CPUUsage{
			OverallPercent: 12.5,
			PerCorePercent: []float64{10, 15},
		},
		CPUValid:         true,
		Temperature:      collector.Temperature{Zone: "cpu-thermal", Celsius: 48.6},
		TemperatureValid: true,
		GPUTemperature:   &collector.GPUTemperature{Celsius: 47.8},
		PMICTemperature:  &collector.PMICTemperature{Celsius: 52.1},
		MemoryValid:      true,
		Memory:           collector.Memory{TotalBytes: 4137000000, AvailableBytes: 2900000000, UsedPercent: 29.9},
		Swap:             collector.Swap{TotalBytes: 104857600, UsedBytes: 0, UsedPercent: 0},
		Disks: []collector.Disk{
			{Mountpoint: "/", TotalBytes: 31000000000, UsedBytes: 8000000000, UsedPercent: 25.8},
		},
		Network: []collector.NetworkInterface{
			{Name: "eth0", RxBytesPerSec: 1240.5, TxBytesPerSec: 302.1},
		},
		Updates: collector.Updates{Count: 3, CheckStatus: collector.UpdatesCheckOK},
		Pihole: collector.Pihole{
			Installed:       true,
			UpdateAvailable: true,
			Components: []collector.PiholeComponent{
				{Name: "core", InstalledVersion: "v6.5", LatestVersion: "v6.6", UpdateAvailable: true},
				{Name: "ftl", InstalledVersion: "v6.8", LatestVersion: "v6.8"},
			},
		},
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
		{"PMIC temperature gauge", "pimonitor_pmic_temperature_celsius 52.1"},
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
		{"pihole update available, core", `pimonitor_pihole_update_available{component="core"} 1`},
		{"pihole up to date, ftl", `pimonitor_pihole_update_available{component="ftl"} 0`},
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
		"pimonitor_pihole_update_available",
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
// or PMIC temperature without vcgencmd, no network section when monitoring is
// disabled (empty slice), no disk section without any mounted filesystem,
// and no per-core CPU family without per-core data.
func TestRenderPrometheusMetrics_OmitsAbsentOptionalFields(t *testing.T) {
	snap := collector.Snapshot{
		CPU:              collector.CPUUsage{OverallPercent: 5},
		CPUValid:         true,
		Temperature:      collector.Temperature{Zone: "cpu-thermal", Celsius: 40},
		TemperatureValid: true,
	}

	body := string(renderPrometheusMetrics(snap))

	for _, absent := range []string{
		"pimonitor_cpu_core_usage_percent",
		"pimonitor_gpu_temperature_celsius",
		"pimonitor_pmic_temperature_celsius",
		"pimonitor_disk_",
		"pimonitor_network_",
		"pimonitor_pihole_",
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
		CPUValid:    true,
		Temperature: collector.Temperature{Zone: "cpu-thermal", Celsius: 40},
	}

	body := string(renderPrometheusMetrics(snap))

	if strings.Contains(body, "pimonitor_temperature_celsius") {
		t.Fatalf("expected no pimonitor_temperature_celsius line when TemperatureValid is false, got:\n%s", body)
	}
}

// TestRenderPrometheusMetrics_OmitsCPUWhenInvalid_AC2 covers issue #243: a
// snapshot without a CPU reading (CPUValid false) leaves both CPU families out
// entirely, even when CPU holds non-zero values, and keeps the other families.
func TestRenderPrometheusMetrics_OmitsCPUWhenInvalid_AC2(t *testing.T) {
	snap := fullSnapshot()
	snap.CPUValid = false
	snap.CPU = collector.CPUUsage{OverallPercent: 12.5, PerCorePercent: []float64{10, 15}}

	body := string(renderPrometheusMetrics(snap))

	for _, line := range strings.Split(body, "\n") {
		for _, family := range []string{"pimonitor_cpu_usage_percent", "pimonitor_cpu_core_usage_percent"} {
			if strings.Contains(line, family) {
				t.Errorf("AC2: line %q mentions %s although CPUValid is false", line, family)
			}
		}
	}
	for _, want := range []string{
		"pimonitor_temperature_celsius{",
		"pimonitor_memory_used_percent 29.9",
		`pimonitor_disk_used_percent{mount="/"} 25.8`,
		"pimonitor_updates_pending 3",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("AC2: output lost %q with CPUValid false, got:\n%s", want, body)
		}
	}
}

// TestRenderPrometheusMetrics_RendersRealZeroCPU_AC3 covers issue #243: with
// CPUValid true a real reading of 0 is written, not mistaken for a missing one.
func TestRenderPrometheusMetrics_RendersRealZeroCPU_AC3(t *testing.T) {
	snap := fullSnapshot()
	snap.CPU = collector.CPUUsage{OverallPercent: 0, PerCorePercent: []float64{0, 0}}

	body := string(renderPrometheusMetrics(snap))

	for _, want := range []string{
		"# HELP pimonitor_cpu_usage_percent ",
		"# TYPE pimonitor_cpu_usage_percent gauge",
		"pimonitor_cpu_usage_percent 0\n",
		`pimonitor_cpu_core_usage_percent{core="0"} 0`,
		`pimonitor_cpu_core_usage_percent{core="1"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("AC3: output missing %q for a real 0 reading, got:\n%s", want, body)
		}
	}
}

// TestRenderPrometheusMetrics_OmitsMemoryWhenInvalid_AC2 covers issue #246: a
// snapshot without a memory reading (MemoryValid false) leaves all memory and
// swap families out entirely, even when Memory and Swap hold non-zero values,
// and keeps the other families.
func TestRenderPrometheusMetrics_OmitsMemoryWhenInvalid_AC2(t *testing.T) {
	snap := fullSnapshot()
	snap.MemoryValid = false

	body := string(renderPrometheusMetrics(snap))

	for _, line := range strings.Split(body, "\n") {
		for _, prefix := range []string{"pimonitor_memory_", "pimonitor_swap_"} {
			if strings.Contains(line, prefix) {
				t.Errorf("AC2: line %q mentions %s although MemoryValid is false", line, prefix)
			}
		}
	}
	for _, want := range []string{
		"pimonitor_cpu_usage_percent 12.5",
		"pimonitor_temperature_celsius{",
		`pimonitor_disk_used_percent{mount="/"} 25.8`,
		"pimonitor_updates_pending 3",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("AC2: output lost %q with MemoryValid false, got:\n%s", want, body)
		}
	}
}

// TestRenderPrometheusMetrics_RendersRealZeroSwap_AC3 covers issue #246: with
// MemoryValid true all six memory and swap families are written, and a host
// without swap (a successful read with SwapTotal 0) gives real 0 samples.
func TestRenderPrometheusMetrics_RendersRealZeroSwap_AC3(t *testing.T) {
	snap := fullSnapshot()
	snap.Swap = collector.Swap{}

	body := string(renderPrometheusMetrics(snap))

	for _, family := range []string{
		"pimonitor_memory_total_bytes",
		"pimonitor_memory_available_bytes",
		"pimonitor_memory_used_percent",
		"pimonitor_swap_total_bytes",
		"pimonitor_swap_used_bytes",
		"pimonitor_swap_used_percent",
	} {
		for _, want := range []string{"# HELP " + family + " ", "# TYPE " + family + " gauge"} {
			if !strings.Contains(body, want) {
				t.Errorf("AC3: output missing %q, got:\n%s", want, body)
			}
		}
	}
	for _, want := range []string{
		"pimonitor_swap_total_bytes 0\n",
		"pimonitor_swap_used_bytes 0\n",
		"pimonitor_swap_used_percent 0\n",
		"pimonitor_memory_total_bytes 4137000000\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("AC3: output missing %q for a real 0 swap reading, got:\n%s", want, body)
		}
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

// ohInt returns a pointer to v for the optional openHAB fields.
func ohInt(v int) *int { return &v }

// ohUint64 returns a pointer to v for the optional openHAB fields.
func ohUint64(v uint64) *uint64 { return &v }

// ohFullStatus is a status in state ok with every value present.
func ohFullStatus() collector.OpenHABStatus {
	return collector.OpenHABStatus{
		State:           collector.OpenHABStateOK,
		Version:         "5.2.4",
		StartLevel:      ohInt(100),
		UptimeSeconds:   ohUint64(86400),
		Process:         &collector.OpenHABProcess{PID: 42, RSSBytes: 5000000000},
		JVM:             &collector.OpenHABJVM{HeapUsedBytes: 300000000, HeapTotalBytes: 6000000000},
		Things:          &collector.OpenHABThings{Total: 6, ByStatus: map[string]int{"ONLINE": 4, "OFFLINE": 2}},
		ItemsCount:      ohInt(250),
		AddonsInstalled: ohInt(12),
	}
}

// ohLines splits the rendered output into lines without the trailing empty one.
func ohLines(out []byte) []string {
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

// ohCount returns how many lines of the output start with prefix.
func ohCount(out []byte, prefix string) int {
	n := 0
	for _, l := range ohLines(out) {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// ohWithPrefix returns the lines of the output that start with prefix.
func ohWithPrefix(out []byte, prefix string) []string {
	var res []string
	for _, l := range ohLines(out) {
		if strings.HasPrefix(l, prefix) {
			res = append(res, l)
		}
	}
	return res
}

// ohSeriesCount returns the number of samples of the metric name, labeled or not.
func ohSeriesCount(out []byte, name string) int {
	return ohCount(out, name+" ") + ohCount(out, name+"{")
}

// ohWant returns 1 when the series is expected, else 0.
func ohWant(present []string, v string) int {
	if slices.Contains(present, v) {
		return 1
	}
	return 0
}

func TestRenderOpenHABMetrics_NoSeriesOutsideReportedStates_AC1(t *testing.T) {
	for _, state := range []string{collector.OpenHABStateNotInstalled, collector.OpenHABStatePending, "", "bogus"} {
		t.Run(fmt.Sprintf("AC1 state %q writes nothing", state), func(t *testing.T) {
			st := ohFullStatus()
			st.State = state
			if out := renderOpenHABMetrics(st); len(out) != 0 {
				t.Fatalf("renderOpenHABMetrics(state %q) = %q, want empty", state, out)
			}
		})
	}
	t.Run("AC1 control: the same values in state ok are written", func(t *testing.T) {
		if out := renderOpenHABMetrics(ohFullStatus()); len(out) == 0 {
			t.Fatal("renderOpenHABMetrics(state ok) is empty, want series")
		}
	})
}

func TestRenderOpenHABMetrics_StateGauges_AC2(t *testing.T) {
	proc := &collector.OpenHABProcess{PID: 1, RSSBytes: 10}
	tests := []struct {
		name    string
		state   string
		process *collector.OpenHABProcess
		up      string
		running string
	}{
		{"ok with process", collector.OpenHABStateOK, proc, "1", "1"},
		{"ok without process", collector.OpenHABStateOK, nil, "1", "0"},
		{"running_unreachable", collector.OpenHABStateUnreachable, proc, "0", "1"},
		{"installed_not_running", collector.OpenHABStateNotRunning, nil, "0", "0"},
	}
	for _, tc := range tests {
		t.Run("AC2 "+tc.name, func(t *testing.T) {
			out := renderOpenHABMetrics(collector.OpenHABStatus{State: tc.state, Process: tc.process})
			got := string(out)
			if want := "pimonitor_openhab_up " + tc.up + "\n"; !strings.Contains(got, want) {
				t.Errorf("output lacks %q:\n%s", want, got)
			}
			if want := "pimonitor_openhab_process_running " + tc.running + "\n"; !strings.Contains(got, want) {
				t.Errorf("output lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestRenderOpenHABMetrics_FullStatus_AC3(t *testing.T) {
	got := string(renderOpenHABMetrics(ohFullStatus()))
	want := []string{
		"pimonitor_openhab_process_resident_bytes 5000000000\n",
		"pimonitor_openhab_jvm_heap_used_bytes 300000000\n",
		"pimonitor_openhab_jvm_heap_total_bytes 6000000000\n",
		"pimonitor_openhab_uptime_seconds 86400\n",
		"pimonitor_openhab_start_level 100\n",
		"pimonitor_openhab_items 250\n",
		"pimonitor_openhab_addons_installed 12\n",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("AC3 output lacks %q:\n%s", w, got)
		}
	}
}

func TestRenderOpenHABMetrics_PartialStatus_AC3(t *testing.T) {
	values := []string{
		"process_resident_bytes", "jvm_heap_used_bytes", "jvm_heap_total_bytes",
		"uptime_seconds", "start_level", "items", "addons_installed",
	}
	tests := []struct {
		name    string
		status  collector.OpenHABStatus
		present []string
	}{
		{"AC3 S2 default install", collector.OpenHABStatus{
			State: collector.OpenHABStateOK, Version: "5.2.4", UptimeSeconds: ohUint64(5), ItemsCount: ohInt(3),
			Process: &collector.OpenHABProcess{RSSBytes: 7},
		}, []string{"process_resident_bytes", "uptime_seconds", "items"}},
		{"AC3 S3 unreachable", collector.OpenHABStatus{
			State: collector.OpenHABStateUnreachable, UptimeSeconds: ohUint64(9),
			Process: &collector.OpenHABProcess{RSSBytes: 8},
		}, []string{"process_resident_bytes", "uptime_seconds"}},
		{"AC3 S4 not running", collector.OpenHABStatus{State: collector.OpenHABStateNotRunning}, nil},
		{"AC3 JVM nil writes neither heap series", collector.OpenHABStatus{
			State: collector.OpenHABStateOK, StartLevel: ohInt(0), AddonsInstalled: ohInt(0),
		}, []string{"start_level", "addons_installed"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := renderOpenHABMetrics(tc.status)
			for _, v := range values {
				n, want := ohSeriesCount(out, "pimonitor_openhab_"+v), ohWant(tc.present, v)
				if n != want {
					t.Errorf("series %s: %d samples, want %d:\n%s", v, n, want, out)
				}
			}
		})
	}
}

// ohThingsLines returns the expected things samples in the order of statuses.
func ohThingsLines(statuses []string, counts map[string]int) []string {
	var want []string
	for _, st := range statuses {
		want = append(want, fmt.Sprintf("pimonitor_openhab_things{status=%q} %d", st, counts[st]))
	}
	return want
}

func TestRenderOpenHABMetrics_ThingsSeries_AC4(t *testing.T) {
	order := []string{"UNINITIALIZED", "INITIALIZING", "UNKNOWN", "ONLINE", "OFFLINE", "REMOVING", "REMOVED"}
	tests := []struct {
		name   string
		things *collector.OpenHABThings
		counts map[string]int
		none   bool
	}{
		{"AC4 mixed counts", &collector.OpenHABThings{Total: 5, ByStatus: map[string]int{"ONLINE": 3, "OFFLINE": 2}},
			map[string]int{"ONLINE": 3, "OFFLINE": 2}, false},
		{"AC4 empty list", &collector.OpenHABThings{ByStatus: map[string]int{}}, nil, false},
		{"AC4 things nil", nil, nil, true},
		{"AC4 key outside the seven", &collector.OpenHABThings{Total: 1, ByStatus: map[string]int{"ONLINE": 1, "BOGUS": 9}},
			map[string]int{"ONLINE": 1}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := renderOpenHABMetrics(collector.OpenHABStatus{State: collector.OpenHABStateOK, Things: tc.things})
			lines := ohWithPrefix(out, "pimonitor_openhab_things{")
			if tc.none {
				if len(lines) != 0 {
					t.Fatalf("things lines = %q, want none", lines)
				}
				return
			}
			want := ohThingsLines(order, tc.counts)
			if !slices.Equal(lines, want) {
				t.Fatalf("things lines = %q, want %q", lines, want)
			}
		})
	}
}

func TestRenderOpenHABMetrics_InfoSeries_AC5(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"AC5 plain version", "5.2.4", `pimonitor_openhab_info{version="5.2.4"} 1`},
		{"AC5 hostile version", "5.2\\4\"} 1\nfake 1", `pimonitor_openhab_info{version="5.2\\4\"} 1\nfake 1"} 1`},
		{"AC5 64 bytes written", strings.Repeat("9", 64), `pimonitor_openhab_info{version="` + strings.Repeat("9", 64) + `"} 1`},
		{"AC5 65 bytes not written", strings.Repeat("9", 65), ""},
		{"AC5 64 quotes written escaped", strings.Repeat(`"`, 64), `pimonitor_openhab_info{version="` + strings.Repeat(`\"`, 64) + `"} 1`},
		{"AC5 empty version", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := renderOpenHABMetrics(collector.OpenHABStatus{State: collector.OpenHABStateOK, Version: tc.version})
			if len(out) == 0 {
				t.Fatal("renderOpenHABMetrics(state ok) is empty, want at least up and process_running")
			}
			assertInfoLines(t, ohWithPrefix(out, "pimonitor_openhab_info"), tc.want)
		})
	}
}

// assertInfoLines checks that info holds exactly the line want, or none when want is empty.
func assertInfoLines(t *testing.T, info []string, want string) {
	t.Helper()
	if want == "" {
		if len(info) != 0 {
			t.Fatalf("info lines = %q, want none", info)
		}
		return
	}
	if len(info) != 1 || info[0] != want {
		t.Fatalf("info lines = %q, want exactly [%q]", info, want)
	}
}

func TestRenderOpenHABMetrics_HelpAndType_AC6(t *testing.T) {
	families := []struct{ name, help string }{
		{"pimonitor_openhab_up", "Whether the openHAB REST API answered (1) or not (0)."},
		{"pimonitor_openhab_process_running", "Whether the openHAB Java process was found (1) or not (0)."},
		{"pimonitor_openhab_process_resident_bytes", "Resident memory of the openHAB process in bytes."},
		{"pimonitor_openhab_jvm_heap_used_bytes", "openHAB JVM heap used in bytes."},
		{"pimonitor_openhab_jvm_heap_total_bytes", "openHAB JVM heap total in bytes."},
		{"pimonitor_openhab_uptime_seconds", "Seconds since the openHAB process started."},
		{"pimonitor_openhab_start_level", "openHAB start level."},
		{"pimonitor_openhab_things", "Number of openHAB Things per status."},
		{"pimonitor_openhab_items", "Number of openHAB Items."},
		{"pimonitor_openhab_addons_installed", "Number of installed openHAB add-ons."},
		{"pimonitor_openhab_info", "openHAB version; the value is always 1."},
	}
	lines := ohLines(renderOpenHABMetrics(ohFullStatus()))
	if len(lines) < 2 {
		t.Fatalf("output has %d lines, want the full status rendered", len(lines))
	}
	pos := 0
	for _, f := range families {
		help := "# HELP " + f.name + " " + f.help
		typ := "# TYPE " + f.name + " gauge"
		idx := slices.Index(lines, help)
		if idx < 0 || slices.Index(lines[idx+1:], help) >= 0 {
			t.Fatalf("AC6 %q appears %d times at %d, want exactly once", help, strings.Count(strings.Join(lines, "\n"), help), idx)
		}
		if idx < pos || idx+1 >= len(lines) || lines[idx+1] != typ {
			t.Fatalf("AC6 %q is not followed by %q in family order", help, typ)
		}
		if idx+2 >= len(lines) || !strings.HasPrefix(lines[idx+2], f.name) {
			t.Fatalf("AC6 family %s has no sample after its header", f.name)
		}
		pos = idx + 2
	}
}

// TestRenderPrometheusMetrics_UpdatesPendingOnlyWhenOK pins that the updates
// family is written only while the latest apt check succeeded.
func TestRenderPrometheusMetrics_UpdatesPendingOnlyWhenOK(t *testing.T) {
	render := func(count int, status string) string {
		snap := fullSnapshot()
		snap.Updates = collector.Updates{Count: count, CheckStatus: status}
		return string(renderPrometheusMetrics(snap))
	}

	t.Run("AC3 ok writes the family", func(t *testing.T) {
		body := render(3, collector.UpdatesCheckOK)
		for _, want := range []string{"# HELP pimonitor_updates_pending ", "# TYPE pimonitor_updates_pending gauge", "pimonitor_updates_pending 3\n"} {
			if !strings.Contains(body, want) {
				t.Errorf("body lacks %q", want)
			}
		}
	})

	t.Run("AC3 ok writes a real 0", func(t *testing.T) {
		if body := render(0, collector.UpdatesCheckOK); !strings.Contains(body, "pimonitor_updates_pending 0\n") {
			t.Errorf("body lacks %q", "pimonitor_updates_pending 0")
		}
	})

	for _, status := range []string{collector.UpdatesCheckNotChecked, collector.UpdatesCheckFailed} {
		t.Run("AC3 "+status+" omits the family", func(t *testing.T) {
			if body := render(3, status); strings.Contains(body, "pimonitor_updates_pending") {
				t.Errorf("body mentions pimonitor_updates_pending with status %q", status)
			}
		})
	}
}
