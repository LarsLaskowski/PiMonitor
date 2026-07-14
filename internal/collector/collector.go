package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/larslaskowski/pimonitor/internal/alert"
	"github.com/larslaskowski/pimonitor/internal/config"
)

// Config controls the collector's polling behavior and history retention.
type Config struct {
	// FastInterval is how often CPU, load average, temperature,
	// memory/swap, disk, and network metrics are sampled.
	FastInterval time.Duration
	// SlowInterval is how often available apt updates are checked. This
	// can be much less frequent than FastInterval since the underlying
	// apt cache is itself only refreshed periodically by a separate,
	// root-privileged systemd timer.
	SlowInterval time.Duration
	// HistoryCapacity is the number of samples retained per metric time
	// series (e.g. FastInterval=5s and HistoryCapacity=720 covers a 1
	// hour rolling window).
	HistoryCapacity int
	// NetworkEnabled toggles network throughput collection entirely.
	NetworkEnabled bool
	// UpdatesStaleThreshold is how old the apt cache may be before the
	// Updates.Stale flag is set.
	UpdatesStaleThreshold time.Duration
	// DistroInfoEnabled toggles whether Snapshot.System.Distribution is
	// populated.
	DistroInfoEnabled bool
	// PiModelEnabled toggles whether Snapshot.System.PiModel is populated.
	PiModelEnabled bool
	// PersistPath is the file metric history is periodically snapshotted
	// to and restored from at startup, so sparklines survive restarts.
	// Empty disables persistence.
	PersistPath string
	// HistoryWindow bounds how far back restored history may reach:
	// persisted points older than this are dropped on load. Zero disables
	// trimming.
	HistoryWindow time.Duration
	// AlertsEnabled toggles the threshold alert engine. When enabled, each
	// fast tick is evaluated against Thresholds into per-metric alert states
	// and transition events served by GET /api/v1/alerts.
	AlertsEnabled bool
	// AlertFor is the alert engine's debounce window: a threshold crossing
	// must persist this long before it is reported as an alert.
	AlertFor time.Duration
	// Thresholds are the warn/critical cutoffs the alert engine evaluates
	// against.
	Thresholds config.Thresholds
}

// History is the collected time series for every metric, keyed by
// mountpoint/interface name where the metric is per-device.
type History struct {
	CPUPercent           []HistoryPoint            `json:"cpu_percent"`
	Load1                []HistoryPoint            `json:"load1"`
	Load5                []HistoryPoint            `json:"load5"`
	Load15               []HistoryPoint            `json:"load15"`
	Temperature          []HistoryPoint            `json:"temperature"`
	MemoryUsedPercent    []HistoryPoint            `json:"memory_used_percent"`
	SwapUsedPercent      []HistoryPoint            `json:"swap_used_percent"`
	DiskUsedPercent      map[string][]HistoryPoint `json:"disk_used_percent,omitempty"`
	NetworkRxBytesPerSec map[string][]HistoryPoint `json:"network_rx_bytes_per_sec,omitempty"`
	NetworkTxBytesPerSec map[string][]HistoryPoint `json:"network_tx_bytes_per_sec,omitempty"`
}

// Collector periodically samples every metric source and keeps the latest
// snapshot plus a bounded in-memory history per metric.
type Collector struct {
	cfg Config

	cpu     *CPUCollector
	loadAvg *LoadAvgCollector
	memory  *MemoryCollector
	disk    *DiskCollector
	network *NetworkCollector
	temp    *TemperatureCollector
	sysInfo *SysInfoCollector
	updates *UpdatesCollector
	uptime  *UptimeCollector

	// alerts is nil when alerting is disabled.
	alerts *alert.Engine

	log *slog.Logger

	mu       sync.RWMutex
	latest   Snapshot
	cpuHist  *RingBuffer[HistoryPoint]
	l1Hist   *RingBuffer[HistoryPoint]
	l5Hist   *RingBuffer[HistoryPoint]
	l15Hist  *RingBuffer[HistoryPoint]
	tempHist *RingBuffer[HistoryPoint]
	memHist  *RingBuffer[HistoryPoint]
	swapHist *RingBuffer[HistoryPoint]
	diskHist map[string]*RingBuffer[HistoryPoint]
	rxHist   map[string]*RingBuffer[HistoryPoint]
	txHist   map[string]*RingBuffer[HistoryPoint]
}

// New creates a Collector wired to the standard Linux metric sources.
func New(cfg Config, log *slog.Logger) *Collector {
	if log == nil {
		log = slog.Default()
	}
	var alerts *alert.Engine
	if cfg.AlertsEnabled {
		alerts = alert.New(cfg.Thresholds, cfg.AlertFor)
	}
	return &Collector{
		cfg:      cfg,
		alerts:   alerts,
		cpu:      NewCPUCollector(),
		loadAvg:  NewLoadAvgCollector(),
		memory:   NewMemoryCollector(),
		disk:     NewDiskCollector(),
		network:  NewNetworkCollector(),
		temp:     NewTemperatureCollector(),
		sysInfo:  NewSysInfoCollector(),
		updates:  NewUpdatesCollector(cfg.UpdatesStaleThreshold),
		uptime:   NewUptimeCollector(),
		log:      log,
		cpuHist:  NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		l1Hist:   NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		l5Hist:   NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		l15Hist:  NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		tempHist: NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		memHist:  NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		swapHist: NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		diskHist: make(map[string]*RingBuffer[HistoryPoint]),
		rxHist:   make(map[string]*RingBuffer[HistoryPoint]),
		txHist:   make(map[string]*RingBuffer[HistoryPoint]),
	}
}

// Run collects an initial sample immediately, then continues sampling on
// FastInterval/SlowInterval until ctx is canceled. Intended to be run in
// its own goroutine.
func (c *Collector) Run(ctx context.Context) {
	c.loadHistory()
	c.collectSysInfo()
	c.fastTick(ctx)
	c.slowTick(ctx)

	// Defense in depth: a non-positive interval panics time.NewTicker.
	// config.Validate rejects such values at startup, but clamp here too so
	// no future caller can crash the collector.
	fastInterval := c.clampInterval(c.cfg.FastInterval, "FastInterval")
	slowInterval := c.clampInterval(c.cfg.SlowInterval, "SlowInterval")

	fastTicker := time.NewTicker(fastInterval)
	defer fastTicker.Stop()
	slowTicker := time.NewTicker(slowInterval)
	defer slowTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush so a clean shutdown (e.g. reboot for updates)
			// loses at most the points since the last fast tick.
			c.persistHistory()
			return
		case <-fastTicker.C:
			c.fastTick(ctx)
		case <-slowTicker.C:
			c.slowTick(ctx)
			c.persistHistory()
		}
	}
}

// clampInterval guards against a non-positive tick interval, which would
// panic time.NewTicker. It substitutes a safe 1s minimum and logs a warning
// naming the offending field.
func (c *Collector) clampInterval(d time.Duration, name string) time.Duration {
	if d <= 0 {
		c.log.Warn("non-positive tick interval clamped to 1s", "field", name, "got", d)
		return time.Second
	}
	return d
}

// Snapshot returns a copy of the most recently collected metrics.
func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// History returns a copy of every metric's retained history.
func (c *Collector) History() History {
	c.mu.RLock()
	defer c.mu.RUnlock()

	h := History{
		CPUPercent:        c.cpuHist.Snapshot(),
		Load1:             c.l1Hist.Snapshot(),
		Load5:             c.l5Hist.Snapshot(),
		Load15:            c.l15Hist.Snapshot(),
		Temperature:       c.tempHist.Snapshot(),
		MemoryUsedPercent: c.memHist.Snapshot(),
		SwapUsedPercent:   c.swapHist.Snapshot(),
	}
	if len(c.diskHist) > 0 {
		h.DiskUsedPercent = make(map[string][]HistoryPoint, len(c.diskHist))
		for k, rb := range c.diskHist {
			h.DiskUsedPercent[k] = rb.Snapshot()
		}
	}
	if len(c.rxHist) > 0 {
		h.NetworkRxBytesPerSec = make(map[string][]HistoryPoint, len(c.rxHist))
		for k, rb := range c.rxHist {
			h.NetworkRxBytesPerSec[k] = rb.Snapshot()
		}
	}
	if len(c.txHist) > 0 {
		h.NetworkTxBytesPerSec = make(map[string][]HistoryPoint, len(c.txHist))
		for k, rb := range c.txHist {
			h.NetworkTxBytesPerSec[k] = rb.Snapshot()
		}
	}
	return h
}

func (c *Collector) collectSysInfo() {
	info := c.sysInfo.Collect()
	if !c.cfg.DistroInfoEnabled {
		info.Distribution = ""
	}
	if !c.cfg.PiModelEnabled {
		info.PiModel = ""
	}
	count, err := c.cpu.CoreCount()
	if err != nil {
		c.log.Warn("could not determine CPU core count", "error", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.latest.System = info
	c.latest.CPUCount = count
}

func (c *Collector) fastTick(ctx context.Context) {
	now := time.Now()

	cpuUsage, err := c.cpu.Collect()
	if err != nil {
		c.log.Warn("cpu collection failed", "error", err)
	}
	load, err := c.loadAvg.Collect()
	if err != nil {
		c.log.Warn("load average collection failed", "error", err)
	}
	temp, gpuTemp, err := c.temp.Collect(ctx)
	if err != nil {
		c.log.Warn("temperature collection failed", "error", err)
	}
	mem, swap, err := c.memory.Collect()
	if err != nil {
		c.log.Warn("memory collection failed", "error", err)
	}
	disks, err := c.disk.Collect()
	if err != nil {
		c.log.Warn("disk collection failed", "error", err)
	}
	var netIfaces []NetworkInterface
	if c.cfg.NetworkEnabled {
		netIfaces, err = c.network.Collect()
		if err != nil {
			c.log.Warn("network collection failed", "error", err)
		}
	}
	uptimeSecs, err := c.uptime.Collect()
	if err != nil {
		c.log.Warn("uptime collection failed", "error", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.latest.Timestamp = now
	c.latest.UptimeSeconds = uptimeSecs
	c.latest.CPU = cpuUsage
	c.latest.Load = load
	c.latest.Temperature = temp
	c.latest.GPUTemperature = gpuTemp
	c.latest.Memory = mem
	c.latest.Swap = swap
	c.latest.Disks = disks
	c.latest.Network = netIfaces

	c.cpuHist.Add(HistoryPoint{Timestamp: now, Value: cpuUsage.OverallPercent})
	c.l1Hist.Add(HistoryPoint{Timestamp: now, Value: load.Load1})
	c.l5Hist.Add(HistoryPoint{Timestamp: now, Value: load.Load5})
	c.l15Hist.Add(HistoryPoint{Timestamp: now, Value: load.Load15})
	c.tempHist.Add(HistoryPoint{Timestamp: now, Value: temp.Celsius})
	c.memHist.Add(HistoryPoint{Timestamp: now, Value: mem.UsedPercent})
	c.swapHist.Add(HistoryPoint{Timestamp: now, Value: swap.UsedPercent})

	for _, d := range disks {
		rb, ok := c.diskHist[d.Mountpoint]
		if !ok {
			rb = NewRingBuffer[HistoryPoint](c.cfg.HistoryCapacity)
			c.diskHist[d.Mountpoint] = rb
		}
		rb.Add(HistoryPoint{Timestamp: now, Value: d.UsedPercent})
	}
	for _, n := range netIfaces {
		rxRB, ok := c.rxHist[n.Name]
		if !ok {
			rxRB = NewRingBuffer[HistoryPoint](c.cfg.HistoryCapacity)
			c.rxHist[n.Name] = rxRB
		}
		rxRB.Add(HistoryPoint{Timestamp: now, Value: n.RxBytesPerSec})

		txRB, ok := c.txHist[n.Name]
		if !ok {
			txRB = NewRingBuffer[HistoryPoint](c.cfg.HistoryCapacity)
			c.txHist[n.Name] = txRB
		}
		txRB.Add(HistoryPoint{Timestamp: now, Value: n.TxBytesPerSec})
	}

	// Evaluate the freshly collected values against the alert thresholds.
	// The engine has its own lock and never calls back into the collector,
	// so doing this while c.mu is held cannot deadlock.
	if c.alerts != nil {
		diskSamples := make([]alert.DiskSample, len(disks))
		for i, d := range disks {
			diskSamples[i] = alert.DiskSample{Mountpoint: d.Mountpoint, UsedPercent: d.UsedPercent}
		}
		c.alerts.Evaluate(alert.Sample{
			Timestamp:    now,
			CPUPercent:   cpuUsage.OverallPercent,
			TemperatureC: temp.Celsius,
			SwapPercent:  swap.UsedPercent,
			Disks:        diskSamples,
		})
	}
}

// Alerts returns the current alert states and recent transition events. When
// alerting is disabled it reports enabled=false with no states or events.
func (c *Collector) Alerts() alert.Report {
	if c.alerts == nil {
		return alert.Report{Enabled: false}
	}
	return c.alerts.Report()
}

func (c *Collector) slowTick(ctx context.Context) {
	updates, err := c.updates.Collect(ctx)
	if err != nil {
		c.log.Warn("updates collection failed", "error", err)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.latest.Updates = updates
}
