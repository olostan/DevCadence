package cognition_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestPortfolioDiff_ACC01_SemanticDeltas(t *testing.T) {
	base := makeTestPortfolio()
	cand := makeTestPortfolio()
	cand.PortfolioID = "port-test-02"
	cand.Revision = 2

	// Modify channel
	cand.Channels[0].MaxConcurrentRequests = 4

	// Add channel
	cand.Channels = append(cand.Channels, protocol.AccessChannel{
		SchemaVersion:         protocol.SchemaVersion1,
		ChannelID:             "chan-cloud-01",
		EndpointID:            "ep-cloud-01",
		Kind:                  protocol.ChannelDirectHTTPAPI,
		SessionMode:           protocol.SessionStatelessPerCall,
		ContextControl:        protocol.ContextControlExactStateless,
		PrefixCache:           protocol.PrefixCacheSessionKV,
		MaxConcurrentRequests: 8,
	})

	// Modify role binding (change endpoint and pool)
	cand.RoleBindings[0].EndpointID = "ep-cli-01"
	cand.RoleBindings[0].BudgetPoolID = "pool-sub"

	// Add role binding with same role but distinct priority (disambiguation check)
	cand.RoleBindings = append(cand.RoleBindings, protocol.RoleBinding{
		Role:             "implementer",
		Priority:         2,
		EndpointID:       "ep-local-01",
		ChannelID:        "chan-local-01",
		BudgetPoolID:     "pool-local",
		ContextProfileID: "prof-local-01",
	})

	// Add budget pool
	cand.BudgetPools = append(cand.BudgetPools, protocol.BudgetPool{
		SchemaVersion:  protocol.SchemaVersion1,
		PoolID:         "pool-metered",
		Name:           "Metered API",
		Regime:         protocol.RegimeMeteredAPI,
		HardLimit:      1000,
		SoftAlertLimit: 800,
		Unit:           protocol.UnitUSDCents,
		Period:         protocol.PeriodBillingCycle,
	})

	diff, err := cognition.DiffPortfolios(base, cand)
	if err != nil {
		t.Fatalf("DiffPortfolios failed: %v", err)
	}

	if !diff.HasChanges {
		t.Fatalf("expected HasChanges: true")
	}
	if diff.FromPortfolioID != base.PortfolioID || diff.ToPortfolioID != cand.PortfolioID {
		t.Errorf("unexpected portfolio ID transition: from %s to %s", diff.FromPortfolioID, diff.ToPortfolioID)
	}
	if diff.FromRevision != 1 || diff.ToRevision != 2 {
		t.Errorf("unexpected revisions: %d -> %d", diff.FromRevision, diff.ToRevision)
	}

	// Channel assertions: chan-cloud-01 (added), chan-local-01 (modified)
	foundAddedChannel := false
	foundModifiedChannel := false
	for _, cd := range diff.ChannelDiffs {
		if cd.ID == "chan-cloud-01" && cd.Delta == cognition.DeltaAdded {
			foundAddedChannel = true
		}
		if cd.ID == "chan-local-01" && cd.Delta == cognition.DeltaModified {
			foundModifiedChannel = true
		}
	}
	if !foundAddedChannel {
		t.Errorf("expected added channel chan-cloud-01 in diff")
	}
	if !foundModifiedChannel {
		t.Errorf("expected modified channel chan-local-01 in diff")
	}

	// Role binding assertions: implementer#2 (added), implementer#1 (modified)
	foundAddedRB := false
	foundModifiedRB := false
	for _, rbd := range diff.RoleBindingDiffs {
		if rbd.ID == "implementer#2" && rbd.Delta == cognition.DeltaAdded {
			foundAddedRB = true
		}
		if rbd.ID == "implementer#1" && rbd.Delta == cognition.DeltaModified {
			foundModifiedRB = true
		}
	}
	if !foundAddedRB {
		t.Errorf("expected added role binding implementer#2 in diff")
	}
	if !foundModifiedRB {
		t.Errorf("expected modified role binding implementer#1 in diff")
	}

	// Budget pool assertions: pool-metered (added)
	foundAddedPool := false
	for _, bpd := range diff.BudgetPoolDiffs {
		if bpd.ID == "pool-metered" && bpd.Delta == cognition.DeltaAdded {
			foundAddedPool = true
		}
	}
	if !foundAddedPool {
		t.Errorf("expected added budget pool pool-metered in diff")
	}
}

func TestPortfolioDiff_ACC02_IdenticalPortfolios(t *testing.T) {
	p := makeTestPortfolio()
	diff, err := cognition.DiffPortfolios(p, p)
	if err != nil {
		t.Fatalf("DiffPortfolios failed: %v", err)
	}
	if diff.HasChanges {
		t.Errorf("expected HasChanges: false for identical portfolios")
	}
	if len(diff.ChannelDiffs) != 0 || len(diff.RoleBindingDiffs) != 0 || len(diff.BudgetPoolDiffs) != 0 || len(diff.PolicyChanges) != 0 {
		t.Errorf("expected empty diff slices for identical portfolios")
	}
}

func TestPortfolioDiff_ACC08_BootstrapZeroBase(t *testing.T) {
	cand := makeTestPortfolio()
	diff, err := cognition.DiffPortfolios(nil, cand)
	if err != nil {
		t.Fatalf("DiffPortfolios failed: %v", err)
	}
	if !diff.HasChanges {
		t.Errorf("expected HasChanges: true for bootstrap diff")
	}
	if diff.FromPortfolioID != "" {
		t.Errorf("expected empty FromPortfolioID, got %q", diff.FromPortfolioID)
	}
	if diff.FromRevision != 0 {
		t.Errorf("expected FromRevision: 0, got %d", diff.FromRevision)
	}
	if diff.ToPortfolioID != cand.PortfolioID {
		t.Errorf("expected ToPortfolioID %q, got %q", cand.PortfolioID, diff.ToPortfolioID)
	}
	if diff.ToRevision != cand.Revision {
		t.Errorf("expected ToRevision %d, got %d", cand.Revision, diff.ToRevision)
	}

	// Verify all items are DeltaAdded
	for _, cd := range diff.ChannelDiffs {
		if cd.Delta != cognition.DeltaAdded {
			t.Errorf("expected DeltaAdded for channel %s, got %s", cd.ID, cd.Delta)
		}
	}
	for _, rbd := range diff.RoleBindingDiffs {
		if rbd.Delta != cognition.DeltaAdded {
			t.Errorf("expected DeltaAdded for role binding %s, got %s", rbd.ID, rbd.Delta)
		}
	}
	for _, bpd := range diff.BudgetPoolDiffs {
		if bpd.Delta != cognition.DeltaAdded {
			t.Errorf("expected DeltaAdded for budget pool %s, got %s", bpd.ID, bpd.Delta)
		}
	}
}

func TestPortfolioDiff_StopRules(t *testing.T) {
	base := makeTestPortfolio()
	_, err := cognition.DiffPortfolios(base, nil)
	if err == nil {
		t.Fatalf("expected error when candidate is nil")
	}
}

func TestPortfolioDiff_MultiPriorityRoleBindings(t *testing.T) {
	// Mutant kill: ensure role bindings with same role but different priorities do not collide
	base := makeTestPortfolio()
	cand := makeTestPortfolio()

	// Base has implementer priority 1 and 2
	base.RoleBindings = append(base.RoleBindings, protocol.RoleBinding{
		Role:             "implementer",
		Priority:         2,
		EndpointID:       "ep-cli-01",
		ChannelID:        "chan-cli-01",
		BudgetPoolID:     "pool-sub",
		ContextProfileID: "prof-cli-01",
	})

	// Candidate removes implementer priority 2 and adds implementer priority 3
	cand.RoleBindings = append(cand.RoleBindings, protocol.RoleBinding{
		Role:             "implementer",
		Priority:         3,
		EndpointID:       "ep-cli-01",
		ChannelID:        "chan-cli-01",
		BudgetPoolID:     "pool-sub",
		ContextProfileID: "prof-cli-01",
	})

	diff, err := cognition.DiffPortfolios(base, cand)
	if err != nil {
		t.Fatalf("DiffPortfolios failed: %v", err)
	}

	var foundRemovedP2, foundAddedP3 bool
	for _, rbd := range diff.RoleBindingDiffs {
		if rbd.ID == "implementer#2" && rbd.Delta == cognition.DeltaRemoved {
			foundRemovedP2 = true
		}
		if rbd.ID == "implementer#3" && rbd.Delta == cognition.DeltaAdded {
			foundAddedP3 = true
		}
	}

	if !foundRemovedP2 {
		t.Errorf("expected implementer#2 to be DeltaRemoved")
	}
	if !foundAddedP3 {
		t.Errorf("expected implementer#3 to be DeltaAdded")
	}
}

func TestPortfolioDiff_InvertedAdditionRemoval(t *testing.T) {
	// Mutant kill: verify removal vs addition direction
	base := makeTestPortfolio()
	cand := makeTestPortfolio()
	// Remove channel from candidate
	cand.Channels = cand.Channels[1:]

	diff, err := cognition.DiffPortfolios(base, cand)
	if err != nil {
		t.Fatalf("DiffPortfolios failed: %v", err)
	}

	removedID := base.Channels[0].ChannelID
	var foundRemoved bool
	for _, cd := range diff.ChannelDiffs {
		if cd.ID == removedID && cd.Delta == cognition.DeltaRemoved {
			foundRemoved = true
		}
		if cd.ID == removedID && cd.Delta == cognition.DeltaAdded {
			t.Errorf("mutant detected: removed channel reported as added")
		}
	}
	if !foundRemoved {
		t.Errorf("expected channel %s to be DeltaRemoved", removedID)
	}
}

func TestPortfolioDiff_DeterministicSorting(t *testing.T) {
	cand := makeTestPortfolio()
	cand.Channels = []protocol.AccessChannel{
		{ChannelID: "chan-z", Kind: protocol.ChannelLocalDaemonSocket, EndpointID: "ep-1", SessionMode: protocol.SessionStatelessPerCall, ContextControl: protocol.ContextControlExactStateless, PrefixCache: protocol.PrefixCacheSessionKV},
		{ChannelID: "chan-a", Kind: protocol.ChannelLocalDaemonSocket, EndpointID: "ep-1", SessionMode: protocol.SessionStatelessPerCall, ContextControl: protocol.ContextControlExactStateless, PrefixCache: protocol.PrefixCacheSessionKV},
		{ChannelID: "chan-m", Kind: protocol.ChannelLocalDaemonSocket, EndpointID: "ep-1", SessionMode: protocol.SessionStatelessPerCall, ContextControl: protocol.ContextControlExactStateless, PrefixCache: protocol.PrefixCacheSessionKV},
	}

	diff, err := cognition.DiffPortfolios(nil, cand)
	if err != nil {
		t.Fatalf("DiffPortfolios failed: %v", err)
	}

	if len(diff.ChannelDiffs) != 3 {
		t.Fatalf("expected 3 channels")
	}
	if diff.ChannelDiffs[0].ID != "chan-a" || diff.ChannelDiffs[1].ID != "chan-m" || diff.ChannelDiffs[2].ID != "chan-z" {
		t.Errorf("channels not sorted alphabetically: %+v", diff.ChannelDiffs)
	}
}
