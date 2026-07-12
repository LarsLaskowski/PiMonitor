package collector

import (
	"testing"
	"time"
)

const netDevFixture1 = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  1000       10    0    0    0     0          0         0     1000       10    0    0    0     0       0          0
  eth0:  5000       50    0    0    0     0          0         0     2000       20    0    0    0     0       0          0
`

const netDevFixture2 = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  1200       12    0    0    0     0          0         0     1200       12    0    0    0     0       0          0
  eth0: 15000      100    0    0    0     0          0         0     7000       40    0    0    0     0       0          0
`

func TestParseNetDev(t *testing.T) {
	counters, err := parseNetDev(netDevFixture1)
	if err != nil {
		t.Fatalf("parseNetDev: %v", err)
	}
	if len(counters) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(counters))
	}
	if counters["eth0"].rxBytes != 5000 || counters["eth0"].txBytes != 2000 {
		t.Fatalf("unexpected eth0 counters: %+v", counters["eth0"])
	}
}

func TestNetworkCollector_Collect(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "net_dev", netDevFixture1)

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &NetworkCollector{
		path: path,
		now:  func() time.Time { return fakeNow },
	}

	first, err := c.Collect()
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if len(first) != 0 {
		t.Fatalf("first Collect should return no interfaces (no prior sample), got %+v", first)
	}

	overwriteTempFile(t, path, netDevFixture2)
	fakeNow = fakeNow.Add(10 * time.Second)

	second, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected 1 interface (lo excluded), got %d: %+v", len(second), second)
	}
	iface := second[0]
	if iface.Name != "eth0" {
		t.Fatalf("expected eth0, got %q", iface.Name)
	}
	wantRx := float64(15000-5000) / 10
	if diffFloat(iface.RxBytesPerSec, wantRx) > 0.01 {
		t.Fatalf("RxBytesPerSec = %v, want %v", iface.RxBytesPerSec, wantRx)
	}
	wantTx := float64(7000-2000) / 10
	if diffFloat(iface.TxBytesPerSec, wantTx) > 0.01 {
		t.Fatalf("TxBytesPerSec = %v, want %v", iface.TxBytesPerSec, wantTx)
	}
}
