package collector

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseThrottled(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   Throttled
	}{
		{
			name:   "no issues",
			output: "throttled=0x0\n",
			want:   Throttled{Raw: "0x0"},
		},
		{
			name:   "under-voltage now and throttled now, both latched since boot",
			output: "throttled=0x50005\n",
			want: Throttled{
				UnderVoltageNow:       true,
				ThrottledNow:          true,
				UnderVoltageSinceBoot: true,
				ThrottledSinceBoot:    true,
				Raw:                   "0x50005",
			},
		},
		{
			name:   "only latched since boot, currently recovered",
			output: "throttled=0x50000",
			want: Throttled{
				UnderVoltageSinceBoot: true,
				ThrottledSinceBoot:    true,
				Raw:                   "0x50000",
			},
		},
		{
			name:   "all flags set",
			output: "throttled=0xf000f\n",
			want: Throttled{
				UnderVoltageNow:          true,
				FrequencyCappedNow:       true,
				ThrottledNow:             true,
				SoftTempLimitNow:         true,
				UnderVoltageSinceBoot:    true,
				FrequencyCappedSinceBoot: true,
				ThrottledSinceBoot:       true,
				SoftTempLimitSinceBoot:   true,
				Raw:                      "0xf000f",
			},
		},
		{
			name:   "frequency capped now, soft temp limit since boot",
			output: "throttled=0x80002\n",
			want: Throttled{
				FrequencyCappedNow:     true,
				SoftTempLimitSinceBoot: true,
				Raw:                    "0x80002",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseThrottled(tt.output)
			if err != nil {
				t.Fatalf("parseThrottled(%q): %v", tt.output, err)
			}
			if got != tt.want {
				t.Fatalf("parseThrottled(%q) =\n  %+v\nwant\n  %+v", tt.output, got, tt.want)
			}
		})
	}
}

func TestParseThrottled_Malformed(t *testing.T) {
	for _, output := range []string{
		"garbage output",
		"throttled=",
		"throttled=notahexnumber",
		"0x50005", // missing prefix
	} {
		if _, err := parseThrottled(output); err == nil {
			t.Fatalf("expected error for malformed output %q", output)
		}
	}
}

func TestThrottledCollector_Collect_NoVcgencmd_AC2(t *testing.T) {
	// A collector whose vcg runner has never found vcgencmd (and whose
	// re-detection window has not elapsed) reports no reading and the
	// missing-vcgencmd error, so the collector can log it once (issue #197).
	now := time.Unix(1_700_000_000, 0)
	c := &ThrottledCollector{
		vcg: &vcgencmdRunner{
			now:        func() time.Time { return now },
			lastDetect: now, // suppress the LookPath retry in this test
		},
	}
	got, err := c.Collect(context.Background())
	if !errors.Is(err, errVcgencmdUnavailable) {
		t.Fatalf("Collect error = %v, want errVcgencmdUnavailable", err)
	}
	if got != nil {
		t.Fatalf("expected no throttled reading without vcgencmd, got %+v", got)
	}
}

func TestThrottledCollector_Collect_Success(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", `echo "throttled=0x50005"`)
	c := &ThrottledCollector{vcg: &vcgencmdRunner{detected: true, path: path}}

	got, err := c.Collect(context.Background())

	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got == nil || got.Raw != "0x50005" || !got.UnderVoltageNow || !got.ThrottledNow {
		t.Fatalf("Collect() = %+v, want decoded reading for throttled=0x50005", got)
	}
}

func TestThrottledCollector_Collect_MalformedOutput(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", `echo "garbage output"`)
	c := &ThrottledCollector{vcg: &vcgencmdRunner{detected: true, path: path}}

	_, err := c.Collect(context.Background())

	if err == nil {
		t.Fatal("expected error for malformed vcgencmd get_throttled output")
	}
}

func TestThrottledCollector_Collect_ExecFails(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", "exit 1")
	c := &ThrottledCollector{vcg: &vcgencmdRunner{detected: true, path: path}}

	_, err := c.Collect(context.Background())

	if err == nil {
		t.Fatal("expected error when vcgencmd get_throttled exits non-zero")
	}
}

func TestThrottledCollector_Collect_NilRunner(t *testing.T) {
	// A collector with no vcg runner at all (vcgencmd disabled for this
	// collector) must also degrade to no reading rather than panicking.
	c := &ThrottledCollector{}

	got, err := c.Collect(context.Background())

	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no throttled reading with nil vcg runner, got %+v", got)
	}
}

// TestParseThrottled_ErrorText pins the exact text of the parse errors: the
// quoted output is bounded to one line of at most vcgencmdDetailMaxLen bytes
// and appears once (the strconv cause is reduced to its reason).
func TestParseThrottled_ErrorText(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    string
		wantNum error
	}{
		{
			name:   "AC3 long output is bounded",
			output: strings.Repeat("x", 250),
			want:   "unexpected vcgencmd get_throttled output: " + strconv.Quote(strings.Repeat("x", 200)+"..."),
		},
		{
			name:   "AC3 multi-line output on one line",
			output: "foo\nbar",
			want:   `unexpected vcgencmd get_throttled output: "foo bar"`,
		},
		{
			name:    "AC4 long unparsable bitmask is bounded",
			output:  "throttled=" + strings.Repeat("z", 250),
			want:    "parse throttled bitmask " + strconv.Quote(strings.Repeat("z", 200)+"...") + ": invalid syntax",
			wantNum: strconv.ErrSyntax,
		},
		{
			name:    "AC4 out-of-range bitmask",
			output:  "throttled=0x" + strings.Repeat("f", 300),
			want:    "parse throttled bitmask " + strconv.Quote("0x"+strings.Repeat("f", 198)+"...") + ": value out of range",
			wantNum: strconv.ErrRange,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseThrottled(tt.output)
			if err == nil {
				t.Fatalf("parseThrottled(%q) error = nil, want %q", tt.output, tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("error text = %q, want %q", err.Error(), tt.want)
			}
			if tt.wantNum != nil && !errors.Is(err, tt.wantNum) {
				t.Errorf("errors.Is(err, %v) = false, err = %v", tt.wantNum, err)
			}
		})
	}
}
