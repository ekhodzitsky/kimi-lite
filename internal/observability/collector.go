// Package observability provides lightweight metrics collection and runtime
// profiling helpers using only the Go standard library.
package observability

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// maxLatencyObservations caps the number of latency samples retained per
// metric key to prevent unbounded memory growth in long-running sessions.
const maxLatencyObservations = 10000

// Collector implements api.MetricsCollector with mutex-protected maps.
// It is safe for concurrent use.
type Collector struct {
	mu        sync.RWMutex
	counters  map[string]int64
	latencies map[string][]time.Duration
}

// Compile-time interface assertion.
var _ api.MetricsCollector = (*Collector)(nil)

// NewCollector creates a new Collector with initialized storage.
func NewCollector() *Collector {
	return &Collector{
		counters:  make(map[string]int64),
		latencies: make(map[string][]time.Duration),
	}
}

// IncCounter increments a counter metric identified by name and optional tags.
// Tags are provided as alternating key/value pairs; an odd number of tags is
// tolerated by treating the final value as empty.
func (c *Collector) IncCounter(name string, tags ...string) {
	if !validMetricName(name) {
		return
	}
	key := metricKey(name, tags...)
	c.mu.Lock()
	c.counters[key]++
	c.mu.Unlock()
}

// RecordLatency records a latency observation for the named metric and tags.
// Observations are kept in a sliding window capped at maxLatencyObservations
// per metric key.
func (c *Collector) RecordLatency(name string, d time.Duration, tags ...string) {
	if !validMetricName(name) {
		return
	}
	key := metricKey(name, tags...)
	c.mu.Lock()
	c.latencies[key] = append(c.latencies[key], d)
	if len(c.latencies[key]) > maxLatencyObservations {
		c.latencies[key] = c.latencies[key][len(c.latencies[key])-maxLatencyObservations:]
	}
	c.mu.Unlock()
}

// RecordError increments the error counter name + ".errors".
func (c *Collector) RecordError(name string) {
	c.IncCounter(name + ".errors")
}

// CounterValue returns the current value of a counter for testing.
func (c *Collector) CounterValue(name string, tags ...string) int64 {
	key := metricKey(name, tags...)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.counters[key]
}

// LatencyCount returns the number of recorded latency observations for testing.
func (c *Collector) LatencyCount(name string, tags ...string) int64 {
	key := metricKey(name, tags...)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return int64(len(c.latencies[key]))
}

// validMetricName rejects empty names and names that contain characters used
// by the metric key encoding, which would corrupt lookup keys.
func validMetricName(name string) bool {
	if name == "" {
		return false
	}
	return !strings.ContainsAny(name, "{}=")
}

// metricKey builds a stable key from a metric name and tag pairs. Tags are
// sorted lexicographically so that equivalent tag sets produce the same key
// regardless of the order supplied by callers.
func metricKey(name string, tags ...string) string {
	if len(tags) == 0 {
		return name
	}

	pairs := make([]string, 0, (len(tags)+1)/2)
	for i := 0; i < len(tags); i += 2 {
		key := tags[i]
		value := ""
		if i+1 < len(tags) {
			value = tags[i+1]
		}
		pairs = append(pairs, key+"="+value)
	}

	sort.Strings(pairs)

	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(p)
	}
	b.WriteByte('}')
	return b.String()
}
