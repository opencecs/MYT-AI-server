package util

import (
	"runtime"
	"testing"
)

func TestParallelWorkers(t *testing.T) {
	cpu := runtime.NumCPU()

	tests := []struct {
		kind string
		min  int // 最小期望值
	}{
		{"io", 4},
		{"cpu", cpu},
		{"udp", 2},
		{"unknown", cpu},
	}

	for _, tt := range tests {
		got := ParallelWorkers(tt.kind)
		if got < tt.min {
			t.Errorf("ParallelWorkers(%q) = %d, want >= %d", tt.kind, got, tt.min)
		}
	}
}

func TestParallelWorkersIO(t *testing.T) {
	n := ParallelWorkers("io")
	expected := runtime.NumCPU() / 2
	if expected < 4 {
		expected = 4
	}
	if n != expected {
		t.Errorf("ParallelWorkers(\"io\") = %d, want %d", n, expected)
	}
}

func TestParallelWorkersUDP(t *testing.T) {
	n := ParallelWorkers("udp")
	expected := runtime.NumCPU() / 2
	if expected < 2 {
		expected = 2
	}
	if n != expected {
		t.Errorf("ParallelWorkers(\"udp\") = %d, want %d", n, expected)
	}
}
