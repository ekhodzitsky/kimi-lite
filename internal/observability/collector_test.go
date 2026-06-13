package observability

import (
	"sync"
	"testing"
	"time"
)

func TestCollectorCounter(t *testing.T) {
	c := NewCollector()
	c.IncCounter("turns", "status", "ok")
	c.IncCounter("turns", "status", "ok")
	c.IncCounter("turns", "status", "err")

	if got := c.CounterValue("turns", "status", "ok"); got != 2 {
		t.Errorf("ok counter = %d, want 2", got)
	}
	if got := c.CounterValue("turns", "status", "err"); got != 1 {
		t.Errorf("err counter = %d, want 1", got)
	}
	if got := c.CounterValue("turns"); got != 0 {
		t.Errorf("untagged counter = %d, want 0", got)
	}
}

func TestCollectorLatency(t *testing.T) {
	c := NewCollector()
	c.RecordLatency("llm.chat", 10*time.Millisecond, "model", "gpt-4")
	c.RecordLatency("llm.chat", 20*time.Millisecond, "model", "gpt-4")

	if got := c.LatencyCount("llm.chat", "model", "gpt-4"); got != 2 {
		t.Errorf("latency count = %d, want 2", got)
	}
}

func TestCollectorTagOrderStability(t *testing.T) {
	c := NewCollector()
	c.IncCounter("tools", "name", "read_file", "status", "ok")
	c.IncCounter("tools", "status", "ok", "name", "read_file")

	if got := c.CounterValue("tools", "name", "read_file", "status", "ok"); got != 2 {
		t.Errorf("counter = %d, want 2", got)
	}
}

func TestCollectorLatencyWindowLimit(t *testing.T) {
	c := NewCollector()
	for i := 0; i < maxLatencyObservations+100; i++ {
		c.RecordLatency("llm.chat", time.Duration(i)*time.Millisecond, "model", "gpt-4")
	}

	if got := c.LatencyCount("llm.chat", "model", "gpt-4"); got != maxLatencyObservations {
		t.Errorf("latency count = %d, want %d", got, maxLatencyObservations)
	}

	c.mu.RLock()
	obs := c.latencies[metricKey("llm.chat", "model", "gpt-4")]
	c.mu.RUnlock()

	first := obs[0]
	last := obs[len(obs)-1]
	wantFirst := time.Duration(100) * time.Millisecond
	wantLast := time.Duration(maxLatencyObservations+99) * time.Millisecond
	if first != wantFirst {
		t.Errorf("oldest observation = %v, want %v", first, wantFirst)
	}
	if last != wantLast {
		t.Errorf("newest observation = %v, want %v", last, wantLast)
	}
}

func TestCollectorInvalidMetricName(t *testing.T) {
	c := NewCollector()
	c.IncCounter("")
	c.IncCounter("bad{name}")
	c.IncCounter("bad=name")
	c.RecordLatency("", time.Millisecond)
	c.RecordError("")

	if got := c.CounterValue(""); got != 0 {
		t.Errorf("empty counter = %d, want 0", got)
	}
	if got := c.CounterValue("bad{name}"); got != 0 {
		t.Errorf("invalid counter = %d, want 0", got)
	}
	if got := c.LatencyCount(""); got != 0 {
		t.Errorf("empty latency = %d, want 0", got)
	}
}

func TestCollectorConcurrentAccess(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup
	workers := 50
	ops := 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				c.IncCounter("concurrent")
				c.RecordLatency("concurrent.latency", time.Duration(j)*time.Microsecond)
				c.CounterValue("concurrent")
				c.LatencyCount("concurrent.latency")
			}
		}(i)
	}
	wg.Wait()

	if got := c.CounterValue("concurrent"); got != int64(workers*ops) {
		t.Errorf("concurrent counter = %d, want %d", got, workers*ops)
	}
	if got := c.LatencyCount("concurrent.latency"); got != int64(workers*ops) {
		t.Errorf("concurrent latency = %d, want %d", got, workers*ops)
	}
}

func BenchmarkMetricKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = metricKey("tool.called", "name", "read_file", "status", "ok")
	}
}

func BenchmarkCollectorIncCounter(b *testing.B) {
	c := NewCollector()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.IncCounter("tool.called", "name", "read_file")
		}
	})
}
