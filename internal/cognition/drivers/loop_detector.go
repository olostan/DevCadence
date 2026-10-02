package drivers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// SemanticLoopDetector detects non-productive reasoning cycles:
// 1. Repeating identical failed tool invocations (same tool name and arguments failing repeatedly).
// 2. Oscillating file edits (cycling file contents back and forth between prior states).
type SemanticLoopDetector struct {
	mu                        sync.Mutex
	maxConsecutiveFailedCalls int
	maxOscillatingEdits       int
	consecutiveFailedKey      string
	consecutiveFailedCount    int
	fileEditHashes            map[string][]string // path -> rolling hashes
	fileOscillationsCount     map[string]int      // path -> count of detected oscillation cycles
}

// LoopDetectorConfig configures thresholds for loop detection.
type LoopDetectorConfig struct {
	MaxConsecutiveFailedCalls int
	MaxOscillatingEdits       int
}

// DefaultLoopDetectorConfig provides sensible defaults.
func DefaultLoopDetectorConfig() LoopDetectorConfig {
	return LoopDetectorConfig{
		MaxConsecutiveFailedCalls: 3,
		MaxOscillatingEdits:       2,
	}
}

// NewSemanticLoopDetector creates a new SemanticLoopDetector.
func NewSemanticLoopDetector(cfg LoopDetectorConfig) *SemanticLoopDetector {
	if cfg.MaxConsecutiveFailedCalls <= 0 {
		cfg.MaxConsecutiveFailedCalls = 3
	}
	if cfg.MaxOscillatingEdits <= 0 {
		cfg.MaxOscillatingEdits = 2
	}
	return &SemanticLoopDetector{
		maxConsecutiveFailedCalls: cfg.MaxConsecutiveFailedCalls,
		maxOscillatingEdits:       cfg.MaxOscillatingEdits,
		fileEditHashes:            make(map[string][]string),
		fileOscillationsCount:     make(map[string]int),
	}
}

// canonicalArguments produces a stable, canonical string for JSON arguments.
func canonicalArguments(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var val any
	if err := json.Unmarshal(raw, &val); err != nil {
		return string(raw)
	}
	canonical, err := json.Marshal(val)
	if err != nil {
		return string(raw)
	}
	return string(canonical)
}

// RecordToolCall inspects a tool execution for repeated identical failures.
// Returns loopDetected=true and a description if an infinite repair/failure loop is identified.
func (d *SemanticLoopDetector) RecordToolCall(call ToolCall, isError bool) (bool, string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := fmt.Sprintf("%s\x00%s", call.Name, canonicalArguments(call.Arguments))

	if isError {
		if d.consecutiveFailedKey == key {
			d.consecutiveFailedCount++
		} else {
			d.consecutiveFailedKey = key
			d.consecutiveFailedCount = 1
		}

		if d.consecutiveFailedCount >= d.maxConsecutiveFailedCalls {
			return true, fmt.Sprintf("semantic loop detected: tool %q with identical arguments failed %d consecutive times (DCI-045)",
				call.Name, d.consecutiveFailedCount)
		}
	} else {
		// A successful tool call resets consecutive failure counter
		if d.consecutiveFailedKey == key {
			d.consecutiveFailedKey = ""
			d.consecutiveFailedCount = 0
		}
	}

	return false, ""
}

// RecordFileEdit inspects a file modification for oscillating edits (A -> B -> A).
// contentHash can be a SHA-256 digest of the new file content or diff.
func (d *SemanticLoopDetector) RecordFileEdit(path string, contentHash string) (bool, string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	history := d.fileEditHashes[path]
	n := len(history)

	// Detect oscillation: if current hash matches hash from 2 turns ago, but differs from previous turn
	// (e.g. state A -> state B -> state A)
	if n >= 2 && history[n-2] == contentHash && history[n-1] != contentHash {
		d.fileOscillationsCount[path]++
		if d.fileOscillationsCount[path] >= d.maxOscillatingEdits {
			return true, fmt.Sprintf("semantic loop detected: file %q oscillated between identical states %d times (DCI-045)",
				path, d.fileOscillationsCount[path])
		}
	}

	// Keep rolling history up to 10 entries per file
	if len(history) >= 10 {
		history = history[1:]
	}
	d.fileEditHashes[path] = append(history, contentHash)

	return false, ""
}

// HashContent is a helper computing the SHA-256 hex digest of a byte slice.
func HashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Reset clears loop detection history.
func (d *SemanticLoopDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.consecutiveFailedKey = ""
	d.consecutiveFailedCount = 0
	d.fileEditHashes = make(map[string][]string)
	d.fileOscillationsCount = make(map[string]int)
}
