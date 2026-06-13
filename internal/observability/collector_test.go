package observability

import (
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
