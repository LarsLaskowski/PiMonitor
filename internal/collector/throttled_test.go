package collector

import (
	"context"
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

func TestThrottledCollector_Collect_NoVcgencmd(t *testing.T) {
	// A collector that has never found vcgencmd (and whose throttled
	// re-detection window has not elapsed) must report no reading rather
	// than failing, so the throttled object is simply omitted off-Pi.
	now := time.Unix(1_700_000_000, 0)
	c := &ThrottledCollector{
		now:                func() time.Time { return now },
		lastVcgencmdDetect: now, // suppress the LookPath retry in this test
	}
	got, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no throttled reading without vcgencmd, got %+v", got)
	}
}
