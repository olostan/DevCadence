package telemetry

import (
	"fmt"
	"sync"
)

// TelemetryCollector provides a thread-safe collector recording telemetry snapshots (REQ-03, INV-01).
type TelemetryCollector struct {
	mu        sync.RWMutex
	snapshots []RunTelemetrySnapshot
	indexByID map[string]int
}

// NewTelemetryCollector creates a new initialized TelemetryCollector.
func NewTelemetryCollector() *TelemetryCollector {
	return &TelemetryCollector{
		snapshots: make([]RunTelemetrySnapshot, 0),
		indexByID: make(map[string]int),
	}
}

// RecordSnapshot records a telemetry snapshot in a thread-safe manner (REQ-03).
func (c *TelemetryCollector) RecordSnapshot(s RunTelemetrySnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	idx, exists := c.indexByID[s.RunID]
	if exists {
		c.snapshots[idx] = s
		return
	}

	c.indexByID[s.RunID] = len(c.snapshots)
	c.snapshots = append(c.snapshots, s)
}

// RecordLayerBreakdown updates the layer breakdown metrics for an existing snapshot (REQ-03).
// If the runID is unknown, an error is returned.
func (c *TelemetryCollector) RecordLayerBreakdown(runID string, b LayerBreakdownMetrics) error {
	if err := b.Validate(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	idx, exists := c.indexByID[runID]
	if !exists {
		return fmt.Errorf("snapshot not found for run ID: %s", runID)
	}

	breakdownCopy := b
	c.snapshots[idx].LayerBreakdown = &breakdownCopy
	return nil
}

// GetSnapshots returns a defensive copy of all recorded snapshots (REQ-03).
func (c *TelemetryCollector) GetSnapshots() []RunTelemetrySnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]RunTelemetrySnapshot, len(c.snapshots))
	for i, s := range c.snapshots {
		result[i] = s
		if s.LayerBreakdown != nil {
			lbCopy := *s.LayerBreakdown
			result[i].LayerBreakdown = &lbCopy
		}
	}
	return result
}
