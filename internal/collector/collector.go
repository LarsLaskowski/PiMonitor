package collector

import (
	"context"
	"log/slog"
	"sort"
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
	// Notifier delivers alert transition events to configured HTTP webhooks.
	// Nil disables notifications. It is started by Run and fed the events each
	// evaluation emits.
	Notifier *alert.Notifier
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

// Since returns h reduced to the points strictly newer than t, for callers
// that already hold everything up to t and only want the delta (see the
// ?since= parameter of GET /api/v1/metrics/history). A zero t returns h
// unchanged. Per-device series are filtered per key, and a device left
// with no points is dropped entirely, matching the `omitempty` shape the
// full response already has for devices that were never seen.
//
// The returned series alias h's backing arrays instead of copying them —
// they are sub-slices of it — so h must not be mutated afterwards. That
// holds for the deep copy History() hands out.
func (h History) Since(t time.Time) History {
	if t.IsZero() {
		return h
	}
	newer := func(p HistoryPoint) bool { return p.Timestamp.After(t) }
	out := History{
		CPUPercent:        dropPrefix(h.CPUPercent, newer),
		Load1:             dropPrefix(h.Load1, newer),
		Load5:             dropPrefix(h.Load5, newer),
		Load15:            dropPrefix(h.Load15, newer),
		Temperature:       dropPrefix(h.Temperature, newer),
		MemoryUsedPercent: dropPrefix(h.MemoryUsedPercent, newer),
		SwapUsedPercent:   dropPrefix(h.SwapUsedPercent, newer),
	}
	filterDevices := func(src map[string][]HistoryPoint) map[string][]HistoryPoint {
		var dst map[string][]HistoryPoint
		for key, points := range src {
			points = dropPrefix(points, newer)
			if len(points) == 0 {
				continue
			}
			if dst == nil {
				dst = make(map[string][]HistoryPoint, len(src))
			}
			dst[key] = points
		}
		return dst
	}
	out.DiskUsedPercent = filterDevices(h.DiskUsedPercent)
	out.NetworkRxBytesPerSec = filterDevices(h.NetworkRxBytesPerSec)
	out.NetworkTxBytesPerSec = filterDevices(h.NetworkTxBytesPerSec)
	return out
}

// dropPrefix returns the sub-slice of points starting at the first point
// keep reports true for. History points are stored oldest first, so every
// time-based filter over them drops a prefix; keep must therefore be
// monotonic in time (false for the oldest points, true from some point
// onwards), which lets the boundary be found by binary search instead of
// walking the whole series. Shared by Since and by importHistory's
// window trim, which differ only in whether the boundary point is kept.
func dropPrefix(points []HistoryPoint, keep func(HistoryPoint) bool) []HistoryPoint {
	return points[sort.Search(len(points), func(i int) bool { return keep(points[i]) }):]
}

// WorstCaseTickOverhead is the most a single fastTick may legitimately run
// over instant /proc-style reads before c.latest is updated. Within
// collectFastTickSamples, TemperatureCollector and ThrottledCollector each
// shell out to vcgencmd (bounded by vcgencmdTimeout) and DiskCollector
// bounds a stalled statfs at defaultStatfsTimeout; all of these run
// sequentially, not concurrently, so the worst case is additive. Callers
// that derive a bound from tick timing (e.g. httpapi's /healthz staleness
// check) should add this on top of FastInterval so a slow-but-healthy tick
// — a hung firmware call or an unresponsive mount, both of which the
// collector deliberately degrades rather than dies on — isn't mistaken for
// a stalled collector.
const WorstCaseTickOverhead = 2*vcgencmdTimeout + defaultStatfsTimeout

// Collector periodically samples every metric source and keeps the latest
// snapshot plus a bounded in-memory history per metric.
type Collector struct {
	cfg Config

	cpu       *CPUCollector
	cpuFreq   *CPUFreqCollector
	loadAvg   *LoadAvgCollector
	memory    *MemoryCollector
	disk      *DiskCollector
	network   *NetworkCollector
	temp      *TemperatureCollector
	throttled *ThrottledCollector
	sysInfo   *SysInfoCollector
	updates   *UpdatesCollector
	uptime    *UptimeCollector

	// alerts is nil when alerting is disabled.
	alerts *alert.Engine
	// notifier is nil when no webhooks are configured, or when alerting is
	// disabled (a disabled engine never produces events, so starting the
	// worker would only leave it idling forever).
	notifier *alert.Notifier

	log *slog.Logger

	// fastInterval is cfg.FastInterval clamped to a safe positive minimum
	// (see clampInterval), computed once at construction so Run's ticker
	// and fastTick's history-eviction window always agree, regardless of
	// whether Run has been started yet (e.g. in tests calling fastTick
	// directly).
	fastInterval time.Duration

	mu     sync.RWMutex
	latest Snapshot
	// historyGen counts fastTicks that have recorded at least one history
	// point. HTTP handlers use it to detect whether the retained history has
	// actually changed since a cached, already-serialised response was built,
	// so a client polling faster than the collector ticks doesn't force a
	// fresh deep-copy-and-encode of the whole window on every request.
	historyGen uint64
	cpuHist    *RingBuffer[HistoryPoint]
	l1Hist     *RingBuffer[HistoryPoint]
	l5Hist     *RingBuffer[HistoryPoint]
	l15Hist    *RingBuffer[HistoryPoint]
	tempHist   *RingBuffer[HistoryPoint]
	memHist    *RingBuffer[HistoryPoint]
	swapHist   *RingBuffer[HistoryPoint]
	diskHist   map[string]*RingBuffer[HistoryPoint]
	rxHist     map[string]*RingBuffer[HistoryPoint]
	txHist     map[string]*RingBuffer[HistoryPoint]

	// persistWG tracks in-flight persistHistory writes so Run's ctx.Done()
	// branch can wait for the final flush before returning.
	persistWG sync.WaitGroup
	// persisting is a buffered try-lock (capacity 1): a successful send means
	// no write is currently in flight. Used to skip an overlapping flush
	// rather than queue it, since the next flush writes newer data anyway.
	persisting chan struct{}
	// writeFile performs the atomic write persistHistory hands off to a
	// background goroutine. Defaults to writeFileAtomic; overridable in
	// tests to control write timing without touching real disk I/O.
	writeFile func(path string, data []byte) error
}

// New creates a Collector wired to the standard Linux metric sources.
func New(cfg Config, log *slog.Logger) *Collector {
	if log == nil {
		log = slog.Default()
	}
	var alerts *alert.Engine
	var notifier *alert.Notifier
	if cfg.AlertsEnabled {
		alerts = alert.New(cfg.Thresholds, cfg.AlertFor)
		notifier = cfg.Notifier
	}
	// TemperatureCollector and ThrottledCollector both shell out to
	// vcgencmd; sharing one runner means vcgencmd is detected once instead
	// of once per collector.
	vcg := newVcgencmdRunner(time.Now)
	c := &Collector{
		cfg: cfg,
		// Disks starts as [] rather than nil so it marshals as [] (not
		// null) before the first fast tick completes, matching docs/API.md.
		latest:    Snapshot{Disks: []Disk{}},
		alerts:    alerts,
		notifier:  notifier,
		cpu:       NewCPUCollector(),
		cpuFreq:   NewCPUFreqCollector(),
		loadAvg:   NewLoadAvgCollector(),
		memory:    NewMemoryCollector(),
		disk:      NewDiskCollector(),
		network:   NewNetworkCollector(),
		temp:      NewTemperatureCollector(vcg),
		throttled: NewThrottledCollector(vcg),
		sysInfo:   NewSysInfoCollector(),
		updates:   NewUpdatesCollector(cfg.UpdatesStaleThreshold),
		uptime:    NewUptimeCollector(),
		log:       log,
		cpuHist:   NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		l1Hist:    NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		l5Hist:    NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		l15Hist:   NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		tempHist:  NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		memHist:   NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		swapHist:  NewRingBuffer[HistoryPoint](cfg.HistoryCapacity),
		diskHist:  make(map[string]*RingBuffer[HistoryPoint]),
		rxHist:    make(map[string]*RingBuffer[HistoryPoint]),
		txHist:    make(map[string]*RingBuffer[HistoryPoint]),

		persisting: make(chan struct{}, 1),
		writeFile:  writeFileAtomic,
	}
	c.fastInterval = c.clampInterval(cfg.FastInterval, "FastInterval")
	return c
}

// Run collects an initial sample immediately, then continues sampling on
// FastInterval/SlowInterval until ctx is canceled. Intended to be run in
// its own goroutine.
func (c *Collector) Run(ctx context.Context) {
	c.loadHistory()
	c.collectSysInfo()
	if c.notifier != nil {
		c.notifier.Start(ctx)
		// Join the delivery worker on shutdown (ctx is canceled by the time
		// Run returns), so no webhook POST is left in flight past exit.
		defer c.notifier.Stop()
	}
	c.fastTick(ctx)
	c.slowTick(ctx)

	// Defense in depth: a non-positive interval panics time.NewTicker.
	// config.Validate rejects such values at startup, but clamp here too so
	// no future caller can crash the collector. c.fastInterval was already
	// clamped in New; only SlowInterval needs it here.
	slowInterval := c.clampInterval(c.cfg.SlowInterval, "SlowInterval")

	fastTicker := time.NewTicker(c.fastInterval)
	defer fastTicker.Stop()
	slowTicker := time.NewTicker(slowInterval)
	defer slowTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush so a clean shutdown (e.g. reboot for updates)
			// loses at most the points since the last fast tick. persistHistory
			// hands the write off to a background goroutine, so wait for it
			// (and any still-running flush from the last slowTick) before
			// returning: cmd/pimonitor/main.go bounds this by its shutdown
			// context, so a stuck fsync cannot hang the process.
			c.persistHistory()
			c.persistWG.Wait()
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

// HistoryGeneration returns a counter that increments every time fastTick
// records a new set of history points. Callers can compare successive
// values to tell whether History() would return anything different without
// having to call it (which deep-copies every ring buffer).
func (c *Collector) HistoryGeneration() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.historyGen
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

// fastTickSamples holds every metric fastTick reads before taking c.mu,
// plus each source's error, so history and alerts can tell a real zero
// value apart from a failed collection.
type fastTickSamples struct {
	now        time.Time
	cpuUsage   CPUUsage
	cpuErr     error
	cpuFreq    []CPUCoreFrequency
	load       LoadAverage
	temp       Temperature
	gpuTemp    *GPUTemperature
	tempErr    error
	throttled  *Throttled
	mem        Memory
	swap       Swap
	memErr     error
	disks      []Disk
	diskErr    error
	netIfaces  []NetworkInterface
	uptimeSecs float64
}

// collectFastTickSamples gathers every metric source fastTick needs. Each
// source's error is logged here (but never aborts the tick) so a single
// failing collector (e.g. no thermal zone on non-Pi hardware) leaves just
// that field at its zero value rather than blocking the others.
func (c *Collector) collectFastTickSamples(ctx context.Context) fastTickSamples {
	var s fastTickSamples
	s.now = time.Now()

	s.cpuUsage, s.cpuErr = c.cpu.Collect()
	if s.cpuErr != nil {
		c.log.Warn("cpu collection failed", "error", s.cpuErr)
	}
	var cpuFreqErr error
	s.cpuFreq, cpuFreqErr = c.cpuFreq.Collect()
	if cpuFreqErr != nil {
		c.log.Warn("cpu frequency collection failed", "error", cpuFreqErr)
	}
	var err error
	s.load, err = c.loadAvg.Collect()
	if err != nil {
		c.log.Warn("load average collection failed", "error", err)
	}
	s.temp, s.gpuTemp, s.tempErr = c.temp.Collect(ctx)
	if s.tempErr != nil {
		c.log.Warn("temperature collection failed", "error", s.tempErr)
	}
	s.throttled, err = c.throttled.Collect(ctx)
	if err != nil {
		c.log.Warn("throttled state collection failed", "error", err)
	}
	s.mem, s.swap, s.memErr = c.memory.Collect()
	if s.memErr != nil {
		c.log.Warn("memory collection failed", "error", s.memErr)
	}
	s.disks, s.diskErr = c.disk.Collect()
	if s.diskErr != nil {
		c.log.Warn("disk collection failed", "error", s.diskErr)
	}
	if c.cfg.NetworkEnabled {
		s.netIfaces, err = c.network.Collect()
		if err != nil {
			c.log.Warn("network collection failed", "error", err)
		}
	}
	s.uptimeSecs, err = c.uptime.Collect()
	if err != nil {
		c.log.Warn("uptime collection failed", "error", err)
	}
	return s
}

func (c *Collector) fastTick(ctx context.Context) {
	s := c.collectFastTickSamples(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.latest.Timestamp = s.now
	c.latest.UptimeSeconds = s.uptimeSecs
	c.latest.CPU = s.cpuUsage
	c.latest.CPUFrequency = s.cpuFreq
	c.latest.Load = s.load
	c.latest.Temperature = s.temp
	c.latest.GPUTemperature = s.gpuTemp
	c.latest.Throttled = s.throttled
	c.latest.Memory = s.mem
	c.latest.Swap = s.swap
	if s.disks == nil {
		// A failed collection (e.g. /proc/mounts unreadable) leaves s.disks
		// nil; keep Snapshot.Disks marshaling as [] rather than null.
		s.disks = []Disk{}
	}
	c.latest.Disks = s.disks
	c.latest.Network = s.netIfaces

	c.cpuHist.Add(HistoryPoint{Timestamp: s.now, Value: s.cpuUsage.OverallPercent})
	c.l1Hist.Add(HistoryPoint{Timestamp: s.now, Value: s.load.Load1})
	c.l5Hist.Add(HistoryPoint{Timestamp: s.now, Value: s.load.Load5})
	c.l15Hist.Add(HistoryPoint{Timestamp: s.now, Value: s.load.Load15})
	c.tempHist.Add(HistoryPoint{Timestamp: s.now, Value: s.temp.Celsius})
	c.memHist.Add(HistoryPoint{Timestamp: s.now, Value: s.mem.UsedPercent})
	c.swapHist.Add(HistoryPoint{Timestamp: s.now, Value: s.swap.UsedPercent})

	c.recordDeviceHistory(s.now, s.disks, s.netIfaces)
	c.historyGen++

	// Evaluate the freshly collected values against the alert thresholds.
	// The engine has its own lock and never calls back into the collector,
	// so doing this while c.mu is held cannot deadlock. Metrics whose
	// collection failed this tick are flagged invalid so a bogus zero can't
	// spuriously clear a real alert; the engine keeps their previous state.
	if c.alerts != nil {
		diskSamples := make([]alert.DiskSample, len(s.disks))
		for i, d := range s.disks {
			diskSamples[i] = alert.DiskSample{Mountpoint: d.Mountpoint, UsedPercent: d.UsedPercent}
		}
		events := c.alerts.Evaluate(alert.Sample{
			Timestamp:        s.now,
			CPUPercent:       s.cpuUsage.OverallPercent,
			CPUValid:         s.cpuErr == nil,
			TemperatureC:     s.temp.Celsius,
			TemperatureValid: s.tempErr == nil,
			MemoryPercent:    s.mem.UsedPercent,
			MemoryValid:      s.memErr == nil,
			SwapPercent:      s.swap.UsedPercent,
			SwapValid:        s.memErr == nil,
			Disks:            diskSamples,
			DisksValid:       s.diskErr == nil,
		})
		// Forward any transition events to the webhook notifier. Notify only
		// enqueues (never blocks), so a slow webhook can't stall collection.
		if c.notifier != nil && len(events) > 0 {
			c.notifier.Notify(events)
		}
	}
}

// recordDeviceHistory adds this tick's samples to the per-device history
// maps (diskHist, rxHist, txHist) and evicts entries for devices that have
// vanished (unplugged USB drive, torn-down veth interface, ...), or those
// maps would otherwise grow without bound as devices churn. A device is
// only evicted once its *newest* sample falls outside the retained history
// window, not merely for being absent from this single tick:
// DiskCollector.Collect already skips mountpoints that fail to stat, so a
// device missing for one tick must keep its history rather than losing it
// immediately. Called from fastTick while c.mu is already held.
func (c *Collector) recordDeviceHistory(now time.Time, disks []Disk, netIfaces []NetworkInterface) {
	diskKeys := make(map[string]struct{}, len(disks))
	for _, d := range disks {
		rb, ok := c.diskHist[d.Mountpoint]
		if !ok {
			rb = NewRingBuffer[HistoryPoint](c.cfg.HistoryCapacity)
			c.diskHist[d.Mountpoint] = rb
		}
		rb.Add(HistoryPoint{Timestamp: now, Value: d.UsedPercent})
		diskKeys[d.Mountpoint] = struct{}{}
	}
	netKeys := make(map[string]struct{}, len(netIfaces))
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
		netKeys[n.Name] = struct{}{}
	}

	historyWindow := c.fastInterval * time.Duration(c.cfg.HistoryCapacity)
	evictStaleSeries(c.diskHist, diskKeys, now, historyWindow)
	evictStaleSeries(c.rxHist, netKeys, now, historyWindow)
	evictStaleSeries(c.txHist, netKeys, now, historyWindow)
}

// Alerts returns the current alert states and recent transition events. When
// alerting is disabled it reports enabled=false with no states or events.
func (c *Collector) Alerts() alert.Report {
	if c.alerts == nil {
		return alert.Report{Enabled: false}
	}
	return c.alerts.Report()
}

// evictStaleSeries deletes entries from hist whose key is not in current
// and whose newest sample is older than window. window <= 0 disables
// eviction (no history window configured to measure staleness against).
func evictStaleSeries(hist map[string]*RingBuffer[HistoryPoint], current map[string]struct{}, now time.Time, window time.Duration) {
	if window <= 0 {
		return
	}
	for key, rb := range hist {
		if _, ok := current[key]; ok {
			continue
		}
		newest, ok := rb.Newest()
		if !ok || now.Sub(newest.Timestamp) > window {
			delete(hist, key)
		}
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
