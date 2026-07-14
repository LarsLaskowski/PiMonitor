package collector

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Metric history is snapshotted to a single compact binary file so
// sparklines survive service restarts and reboots. The format ("PIMH v1",
// all integers little-endian) is:
//
//	magic   [4]byte "PIMH"
//	version uint16
//	series  uint32                       number of series records
//	per series:
//	  kind   uint8                       one of the series* constants
//	  keyLen uint16, key []byte          mountpoint/interface ("" for scalars)
//	  points uint32
//	  per point:
//	    timestamp int64                  Unix milliseconds
//	    value     float64                IEEE 754 bits
//
// Timestamps are truncated to millisecond precision, which is far finer
// than any supported poll interval. Unknown series kinds are skipped on
// decode so older binaries tolerate files written by newer ones that only
// add kinds; any other format change must bump historyVersion.

const (
	historyMagic   = "PIMH"
	historyVersion = 1

	// Decode limits so a corrupt or malicious file cannot cause huge
	// allocations. maxPointsPerSeries covers a week of 1-second samples,
	// well beyond any sensible history_window_minutes.
	maxSeries          = 1 << 12
	maxKeyLen          = 1 << 12
	maxPointsPerSeries = 1 << 21

	pointSize = 16 // int64 timestamp + float64 value
)

// Series kinds. Values are part of the on-disk format; never renumber.
const (
	seriesCPUPercent uint8 = iota
	seriesLoad1
	seriesLoad5
	seriesLoad15
	seriesTemperature
	seriesMemoryUsedPercent
	seriesSwapUsedPercent
	seriesDiskUsedPercent
	seriesNetworkRxBytesPerSec
	seriesNetworkTxBytesPerSec
)

// historySeries is one time series flattened out of a History for
// serialization: a kind, an optional map key, and the points.
type historySeries struct {
	kind   uint8
	key    string
	points []HistoryPoint
}

// historySeriesList flattens h into series records, map series sorted by
// key so encoding is deterministic.
func historySeriesList(h History) []historySeries {
	list := []historySeries{
		{seriesCPUPercent, "", h.CPUPercent},
		{seriesLoad1, "", h.Load1},
		{seriesLoad5, "", h.Load5},
		{seriesLoad15, "", h.Load15},
		{seriesTemperature, "", h.Temperature},
		{seriesMemoryUsedPercent, "", h.MemoryUsedPercent},
		{seriesSwapUsedPercent, "", h.SwapUsedPercent},
	}
	appendMap := func(kind uint8, m map[string][]HistoryPoint) {
		keys := make([]string, 0, len(m))
		for k := range m {
			if len(k) <= maxKeyLen {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			list = append(list, historySeries{kind, k, m[k]})
		}
	}
	appendMap(seriesDiskUsedPercent, h.DiskUsedPercent)
	appendMap(seriesNetworkRxBytesPerSec, h.NetworkRxBytesPerSec)
	appendMap(seriesNetworkTxBytesPerSec, h.NetworkTxBytesPerSec)
	return list
}

// seriesToHistory is the inverse of historySeriesList. Series of unknown
// kind are ignored.
func seriesToHistory(list []historySeries) History {
	var h History
	mapEntry := func(m *map[string][]HistoryPoint, s historySeries) {
		if *m == nil {
			*m = make(map[string][]HistoryPoint)
		}
		(*m)[s.key] = s.points
	}
	for _, s := range list {
		switch s.kind {
		case seriesCPUPercent:
			h.CPUPercent = s.points
		case seriesLoad1:
			h.Load1 = s.points
		case seriesLoad5:
			h.Load5 = s.points
		case seriesLoad15:
			h.Load15 = s.points
		case seriesTemperature:
			h.Temperature = s.points
		case seriesMemoryUsedPercent:
			h.MemoryUsedPercent = s.points
		case seriesSwapUsedPercent:
			h.SwapUsedPercent = s.points
		case seriesDiskUsedPercent:
			mapEntry(&h.DiskUsedPercent, s)
		case seriesNetworkRxBytesPerSec:
			mapEntry(&h.NetworkRxBytesPerSec, s)
		case seriesNetworkTxBytesPerSec:
			mapEntry(&h.NetworkTxBytesPerSec, s)
		}
	}
	return h
}

// encodeHistory serializes h into the PIMH binary format.
func encodeHistory(h History) []byte {
	list := historySeriesList(h)

	size := len(historyMagic) + 2 + 4
	for _, s := range list {
		size += 1 + 2 + len(s.key) + 4 + len(s.points)*pointSize
	}

	buf := make([]byte, 0, size)
	buf = append(buf, historyMagic...)
	buf = binary.LittleEndian.AppendUint16(buf, historyVersion)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(list)))
	for _, s := range list {
		buf = append(buf, s.kind)
		buf = binary.LittleEndian.AppendUint16(buf, uint16(len(s.key)))
		buf = append(buf, s.key...)
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(s.points)))
		for _, p := range s.points {
			buf = binary.LittleEndian.AppendUint64(buf, uint64(p.Timestamp.UnixMilli()))
			buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(p.Value))
		}
	}
	return buf
}

var errHistoryTruncated = errors.New("history file truncated")

// historyDecoder reads little-endian values from a byte slice, tracking a
// sticky error so callers can decode a whole record and check once.
type historyDecoder struct {
	data []byte
	off  int
	err  error
}

func (d *historyDecoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n < 0 || len(d.data)-d.off < n {
		d.err = errHistoryTruncated
		return nil
	}
	b := d.data[d.off : d.off+n]
	d.off += n
	return b
}

func (d *historyDecoder) uint8() uint8 {
	if b := d.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (d *historyDecoder) uint16() uint16 {
	if b := d.take(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (d *historyDecoder) uint32() uint32 {
	if b := d.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

// decodeHistory parses the PIMH binary format produced by encodeHistory.
func decodeHistory(data []byte) (History, error) {
	d := &historyDecoder{data: data}

	magic := d.take(len(historyMagic))
	if d.err != nil || string(magic) != historyMagic {
		return History{}, errors.New("not a PiMonitor history file (bad magic)")
	}
	if version := d.uint16(); d.err == nil && version != historyVersion {
		return History{}, fmt.Errorf("unsupported history file version %d", version)
	}
	count := d.uint32()
	if d.err == nil && count > maxSeries {
		return History{}, fmt.Errorf("history file declares %d series (limit %d)", count, maxSeries)
	}

	list := make([]historySeries, 0, count)
	for i := uint32(0); i < count && d.err == nil; i++ {
		kind := d.uint8()
		keyLen := d.uint16()
		if d.err == nil && int(keyLen) > maxKeyLen {
			return History{}, fmt.Errorf("history file series key length %d exceeds limit %d", keyLen, maxKeyLen)
		}
		key := string(d.take(int(keyLen)))
		n := d.uint32()
		if d.err == nil && n > maxPointsPerSeries {
			return History{}, fmt.Errorf("history file series declares %d points (limit %d)", n, maxPointsPerSeries)
		}
		raw := d.take(int(n) * pointSize)
		if d.err != nil {
			break
		}
		points := make([]HistoryPoint, n)
		for j := range points {
			ms := int64(binary.LittleEndian.Uint64(raw[j*pointSize:]))
			bits := binary.LittleEndian.Uint64(raw[j*pointSize+8:])
			points[j] = HistoryPoint{Timestamp: time.UnixMilli(ms), Value: math.Float64frombits(bits)}
		}
		list = append(list, historySeries{kind: kind, key: key, points: points})
	}
	if d.err != nil {
		return History{}, d.err
	}
	if d.off != len(d.data) {
		return History{}, errors.New("trailing data in history file")
	}
	return seriesToHistory(list), nil
}

// writeFileAtomic writes data to path via a temp file in the same
// directory plus rename, so a crash mid-write never leaves a partially
// written history file behind, and creates the parent directory if needed.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	err = func() error {
		if _, err := tmp.Write(data); err != nil {
			return err
		}
		if err := tmp.Sync(); err != nil {
			return err
		}
		return tmp.Close()
	}()
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	return nil
}

// persistHistory snapshots the current metric history to cfg.PersistPath
// with an atomic write. Failures are logged, not fatal: persistence is
// best-effort and must never take down metric collection.
func (c *Collector) persistHistory() {
	if c.cfg.PersistPath == "" {
		return
	}
	if err := writeFileAtomic(c.cfg.PersistPath, encodeHistory(c.History())); err != nil {
		c.log.Warn("could not persist metric history", "path", c.cfg.PersistPath, "error", err)
	}
}

// loadHistory restores metric history from cfg.PersistPath, if present,
// dropping points older than the configured history window. A missing file
// is normal (first start); an unreadable or corrupt one is logged and
// ignored so the collector still starts with empty history.
func (c *Collector) loadHistory() {
	if c.cfg.PersistPath == "" {
		return
	}
	data, err := os.ReadFile(c.cfg.PersistPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		c.log.Warn("could not read persisted metric history", "path", c.cfg.PersistPath, "error", err)
		return
	}
	h, err := decodeHistory(data)
	if err != nil {
		c.log.Warn("ignoring invalid persisted metric history", "path", c.cfg.PersistPath, "error", err)
		return
	}
	c.importHistory(h, time.Now())
	c.log.Info("restored persisted metric history", "path", c.cfg.PersistPath)
}

// importHistory replaces the ring buffers' contents with h, dropping
// points older than the history window (points are stored oldest first, so
// trimming strips a prefix). Map series that end up empty after trimming
// are omitted entirely, matching how live collection only creates buffers
// for devices it has actually seen.
func (c *Collector) importHistory(h History, now time.Time) {
	var cutoff time.Time
	if c.cfg.HistoryWindow > 0 {
		cutoff = now.Add(-c.cfg.HistoryWindow)
	}
	trim := func(points []HistoryPoint) []HistoryPoint {
		i := 0
		for i < len(points) && points[i].Timestamp.Before(cutoff) {
			i++
		}
		return points[i:]
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cpuHist.Fill(trim(h.CPUPercent))
	c.l1Hist.Fill(trim(h.Load1))
	c.l5Hist.Fill(trim(h.Load5))
	c.l15Hist.Fill(trim(h.Load15))
	c.tempHist.Fill(trim(h.Temperature))
	c.memHist.Fill(trim(h.MemoryUsedPercent))
	c.swapHist.Fill(trim(h.SwapUsedPercent))

	importMap := func(dst map[string]*RingBuffer[HistoryPoint], src map[string][]HistoryPoint) {
		for key, points := range src {
			points = trim(points)
			if len(points) == 0 {
				continue
			}
			rb := NewRingBuffer[HistoryPoint](c.cfg.HistoryCapacity)
			rb.Fill(points)
			dst[key] = rb
		}
	}
	importMap(c.diskHist, h.DiskUsedPercent)
	importMap(c.rxHist, h.NetworkRxBytesPerSec)
	importMap(c.txHist, h.NetworkTxBytesPerSec)
}
