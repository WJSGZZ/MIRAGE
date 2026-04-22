package daemon

import (
	"sync/atomic"
	"time"
)

// Stats holds cumulative traffic counters for one local MIRAGE runtime.
type Stats struct {
	startedAt      atomic.Int64
	uploadBytes    atomic.Int64
	downloadBytes  atomic.Int64
}

func NewStats() *Stats {
	s := &Stats{}
	s.Reset()
	return s
}

func (s *Stats) Reset() {
	s.startedAt.Store(time.Now().UnixNano())
	s.uploadBytes.Store(0)
	s.downloadBytes.Store(0)
}

func (s *Stats) AddUpload(n int64) {
	if n > 0 {
		s.uploadBytes.Add(n)
	}
}

func (s *Stats) AddDownload(n int64) {
	if n > 0 {
		s.downloadBytes.Add(n)
	}
}

func (s *Stats) Snapshot() Snapshot {
	started := time.Unix(0, s.startedAt.Load())
	up := s.uploadBytes.Load()
	down := s.downloadBytes.Load()
	uptime := time.Since(started)
	if uptime < time.Second {
		uptime = time.Second
	}
	seconds := int64(uptime.Seconds())
	if seconds <= 0 {
		seconds = 1
	}
	return Snapshot{
		StartedAt:       started,
		UptimeSeconds:   seconds,
		UploadBytes:     up,
		DownloadBytes:   down,
		UploadRateBps:   up / seconds,
		DownloadRateBps: down / seconds,
	}
}

type Snapshot struct {
	StartedAt       time.Time
	UptimeSeconds   int64
	UploadBytes     int64
	DownloadBytes   int64
	UploadRateBps   int64
	DownloadRateBps int64
}
