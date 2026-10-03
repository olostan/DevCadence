package compiler

import (
	"slices"
	"strings"
	"sync"

	"github.com/olostan/DevCadence/internal/protocol"
)

// CapsuleManager manages the non-authoritative cognitive state across multi-turn sessions.
// Per PROTOCOLS §10B and ADR-0020 §8:
// The Cognitive State Capsule carries derived hypotheses, active TODOs, intermediate decisions,
// open questions, and evidence dependencies; it does NOT certify truth or replace canonical state.
type CapsuleManager struct {
	mu                    sync.RWMutex
	hypotheses            []string
	activeTODOs           []string
	intermediateDecisions []string
	openQuestions         []string
	evidenceDependencies  []string
}

// NewCapsuleManager initializes an empty CapsuleManager.
func NewCapsuleManager() *CapsuleManager {
	return &CapsuleManager{
		hypotheses:            make([]string, 0),
		activeTODOs:           make([]string, 0),
		intermediateDecisions: make([]string, 0),
		openQuestions:         make([]string, 0),
		evidenceDependencies:  make([]string, 0),
	}
}

// AddHypothesis records an unverified hypothesis.
func (c *CapsuleManager) AddHypothesis(h string) {
	trimmed := strings.TrimSpace(h)
	if trimmed == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.hypotheses, trimmed) {
		c.hypotheses = append(c.hypotheses, trimmed)
	}
}

// ResolveHypothesis removes a resolved or falsified hypothesis.
func (c *CapsuleManager) ResolveHypothesis(h string) {
	trimmed := strings.TrimSpace(h)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hypotheses = slices.DeleteFunc(c.hypotheses, func(item string) bool {
		return item == trimmed
	})
}

// AddTODO registers an active task or work item.
func (c *CapsuleManager) AddTODO(todo string) {
	trimmed := strings.TrimSpace(todo)
	if trimmed == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.activeTODOs, trimmed) {
		c.activeTODOs = append(c.activeTODOs, trimmed)
	}
}

// CompleteTODO marks a task completed by removing it from active TODOs.
func (c *CapsuleManager) CompleteTODO(todo string) {
	trimmed := strings.TrimSpace(todo)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeTODOs = slices.DeleteFunc(c.activeTODOs, func(item string) bool {
		return item == trimmed
	})
}

// RecordDecision logs an intermediate engineering decision made during cognition.
func (c *CapsuleManager) RecordDecision(d string) {
	trimmed := strings.TrimSpace(d)
	if trimmed == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.intermediateDecisions, trimmed) {
		c.intermediateDecisions = append(c.intermediateDecisions, trimmed)
	}
}

// AddOpenQuestion records an unresolved question.
func (c *CapsuleManager) AddOpenQuestion(q string) {
	trimmed := strings.TrimSpace(q)
	if trimmed == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.openQuestions, trimmed) {
		c.openQuestions = append(c.openQuestions, trimmed)
	}
}

// AnswerQuestion removes an answered open question.
func (c *CapsuleManager) AnswerQuestion(q string) {
	trimmed := strings.TrimSpace(q)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.openQuestions = slices.DeleteFunc(c.openQuestions, func(item string) bool {
		return item == trimmed
	})
}

// TrackEvidenceDependency records that the current cognitive state relies on an EvidenceLease ID.
func (c *CapsuleManager) TrackEvidenceDependency(leaseID string) {
	trimmed := strings.TrimSpace(leaseID)
	if trimmed == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.evidenceDependencies, trimmed) {
		c.evidenceDependencies = append(c.evidenceDependencies, trimmed)
	}
}

// RemoveEvidenceDependency untracks an evidence dependency when the lease is released/invalidated.
func (c *CapsuleManager) RemoveEvidenceDependency(leaseID string) {
	trimmed := strings.TrimSpace(leaseID)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evidenceDependencies = slices.DeleteFunc(c.evidenceDependencies, func(item string) bool {
		return item == trimmed
	})
}

// Snapshot returns an immutable deep copy of the current protocol.CognitiveStateCapsule.
func (c *CapsuleManager) Snapshot() protocol.CognitiveStateCapsule {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return protocol.CognitiveStateCapsule{
		Hypotheses:            append([]string(nil), c.hypotheses...),
		ActiveTODOs:           append([]string(nil), c.activeTODOs...),
		IntermediateDecisions: append([]string(nil), c.intermediateDecisions...),
		OpenQuestions:         append([]string(nil), c.openQuestions...),
		EvidenceDependencies:  append([]string(nil), c.evidenceDependencies...),
	}
}

// Restore resets the capsule state to a previously saved protocol.CognitiveStateCapsule.
func (c *CapsuleManager) Restore(capsule protocol.CognitiveStateCapsule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hypotheses = append([]string(nil), capsule.Hypotheses...)
	c.activeTODOs = append([]string(nil), capsule.ActiveTODOs...)
	c.intermediateDecisions = append([]string(nil), capsule.IntermediateDecisions...)
	c.openQuestions = append([]string(nil), capsule.OpenQuestions...)
	c.evidenceDependencies = append([]string(nil), capsule.EvidenceDependencies...)
}

// Clear resets the capsule to empty.
func (c *CapsuleManager) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hypotheses = make([]string, 0)
	c.activeTODOs = make([]string, 0)
	c.intermediateDecisions = make([]string, 0)
	c.openQuestions = make([]string, 0)
	c.evidenceDependencies = make([]string, 0)
}
