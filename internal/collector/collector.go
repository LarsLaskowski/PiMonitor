package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"
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
	return &Collector{
		cfg:      cfg,
		cpu:      NewCPUCollector(),
		loadAvg:  NewLoadAvgCollector(),
		memory:   NewMemoryCollector(),
		disk:     NewDiskCollector(),
		network:  NewNetworkCollector(),
		temp:     NewTemperatureCollector(),
		sysInfo:  NewSysInfoCollector(),
		updates:  NewUpdatesCollector(cfg.UpdatesStaleThreshold),
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
	c.collectSysInfo()
	c.fastTick(ctx)
	c.slowTick(ctx)

	fastTicker := time.NewTicker(c.cfg.FastInterval)
	defer fastTicker.Stop()
	slowTicker := time.NewTicker(c.cfg.SlowInterval)
	defer slowTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fastTicker.C:
			c.fastTick(ctx)
		case <-slowTicker.C:
			c.slowTick(ctx)
		}
	}
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

	c.mu.Lock()
	defer c.mu.Unlock()

	c.latest.Timestamp = now
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
