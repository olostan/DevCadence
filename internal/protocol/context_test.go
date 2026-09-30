package protocol_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

func validContextProfile() *protocol.ContextProfile {
	return &protocol.ContextProfile{
		SchemaVersion:             protocol.SchemaVersion1,
		ProfileID:                 "prof_1",
		EndpointID:                "ep_1",
		ChannelID:                 "chan_1",
		Runtime:                   "ollama",
		ModelRef:                  "qwen2.5-coder:32b",
		Revision:                  1,
		DeclaredWindowTokens:      32768,
		RuntimeWindowTokens:       32768,
		WorkloadEnvelopes:         []protocol.WorkloadEnvelope{
			{
				Workload:        protocol.WorkloadImplementation,
				EffectiveTokens: 24000,
				CalibrationTask: "task_1",
				CalibrationDate: "2026-09-30T00:00:00Z",
				ConfidenceLevel: "verified",
			},
		},
		TargetResidentTokens:      16000,
		HardResidentCeilingTokens: 24000,
		ProtectedCoreLimitTokens:  2000,
		ContractLimitTokens:       4000,
		MaxSingleLeaseTokens:      6000,
		OutputReserveTokens:       4000,
		ToolTailReserveTokens:     2000,
		AccountingMethod:          protocol.AccountingExactBPE,
		EstimateUncertaintyRatio:  0.05,
		ObservedContextControl:    protocol.ContextControlExactStateless,
		ObservedPrefixCache:       protocol.PrefixCacheSessionKV,
	}
}

func validEvidenceLease() *protocol.EvidenceLease {
	return &protocol.EvidenceLease{
		SchemaVersion:       protocol.SchemaVersion1,
		LeaseID:             "lease_1",
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      "58869d99635ee0d05b5fe30e3b152dacddc12445",
		WorktreeID:          "wt_1",
		FilePath:            "internal/setup/doctor.go",
		Locator:             "L815-L835",
		ContentDigest:       "sha256:2fbce30fb68b9fd298ea9914785690e3959fd7ec42f4a2ab093984a331d90f5a",
		AcquisitionQuestion: "Question?",
		AcquisitionReason:   "Reason",
		Content:             "func Test() {}",
		TokenCount:          28,
		AccountingMethod:    protocol.AccountingExactBPE,
		Status:              protocol.LeaseStatusActive,
		AcquiredAt:          "2026-09-30T00:00:00Z",
	}
}

func validContextManifest() *protocol.ContextManifest {
	return &protocol.ContextManifest{
		SchemaVersion:        protocol.SchemaVersion1,
		ManifestID:           "manifest_1",
		TaskID:               "task_1",
		WorkPackageID:        "WP-M3C-1",
		WorkPackageRevision:  1,
		WorkPackageDigest:    "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Role:                 "implementer",
		BaseCommit:           "58869d99635ee0d05b5fe30e3b152dacddc12445",
		ProjectStateRevision: "rev_1",
		MappingVersion:       "v1.0",
		SourceRevision:       "58869d99635ee0d05b5fe30e3b152dacddc12445",
		ReadEnvelope:         []string{"internal/*"},
		WriteScope:           []string{"internal/protocol/*"},
		Domains:              []string{"cognition"},
		RiskTags:             []string{"drift"},
		MandatoryClauses: []protocol.MandatoryClauseRef{
			{
				ClauseID:      "DCI-018",
				SourceDoc:     "docs/INVARIANTS.md",
				Revision:      "v1.0",
				ContentDigest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		InitialEvidenceRefs: []string{"git:58869d9:internal/protocol/credentials.go"},
		Assumptions: []protocol.Assumption{
			{
				ID:        "asm_1",
				Statement: "Assumption statement",
				Status:    protocol.AssumptionVerified,
				Material:  true,
			},
		},
		ExplicitQuestions:   []string{"Q?"},
		ExpansionTriggers:   []string{"trigger"},
		AdmissionProvenance: []string{"automated_resolver_v1"},
		ContextProfileID:    "prof_1",
		BudgetPoolID:        "pool_1",
	}
}

func validContextPack() *protocol.ContextPack {
	lease := *validEvidenceLease()
	return &protocol.ContextPack{
		SchemaVersion:      protocol.SchemaVersion1,
		PackID:             "pack_1",
		ManifestID:         "manifest_1",
		ManifestRevision:   1,
		RoleCore:           "Role core",
		ExecutionContract:  "Execution contract",
		NormativeClauses:   []string{"DCI-018"},
		CognitiveState: protocol.CognitiveStateCapsule{
			Hypotheses:            []string{"H1"},
			ActiveTODOs:           []string{"T1"},
			IntermediateDecisions: []string{"D1"},
			OpenQuestions:         []string{},
			EvidenceDependencies:  []string{"lease_1"},
		},
		EvidenceWorkingSet: []protocol.EvidenceLease{lease},
		EphemeralTail: protocol.EphemeralTailBlock{
			RecentToolExchanges: []string{"Tool output"},
			CurrentAction:       "Action",
		},
		TokenAccounting: protocol.TokenAccountingBreakdown{
			RoleTokens:          100,
			ContractTokens:      500,
			NormativeTokens:     80,
			StateTokens:         120,
			EvidenceTokens:      28,
			TailTokens:          50,
			OutputReserveTokens: 2000,
			TotalResidentTokens: 878,
			AccountingMethod:    protocol.AccountingExactBPE,
		},
		AdmittedObjectDigests: map[string]string{
			"manifest": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		PackDigest:      "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		CoverageSummary: "Full coverage for WP-M3C-1 requirements",
		Status:          protocol.PackStatusReady,
	}
}

func TestContextProfileValidation(t *testing.T) {
	t.Run("valid profile passes", func(t *testing.T) {
		cp := validContextProfile()
		if err := cp.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
		if cp.RecordKind() != "ContextProfile" {
			t.Errorf("record kind: got %q, want ContextProfile", cp.RecordKind())
		}
	})

	t.Run("target tokens exceeding ceiling rejected", func(t *testing.T) {
		cp := validContextProfile()
		cp.TargetResidentTokens = cp.HardResidentCeilingTokens + 1
		if err := cp.Validate(); err == nil {
			t.Fatal("expected error when target > ceiling, got nil")
		}
	})

	t.Run("ceiling plus reserves exceeding runtime window rejected", func(t *testing.T) {
		cp := validContextProfile()
		cp.HardResidentCeilingTokens = 30000
		// 30000 + 4000 + 2000 = 36000 > 32768
		if err := cp.Validate(); err == nil {
			t.Fatal("expected error when ceiling + reserves > runtime window, got nil")
		}
	})

	t.Run("invalid uncertainty ratio rejected", func(t *testing.T) {
		cp := validContextProfile()
		cp.EstimateUncertaintyRatio = 1.5
		if err := cp.Validate(); err == nil {
			t.Fatal("expected error on uncertainty ratio > 1.0, got nil")
		}
	})

	t.Run("empty calibration evidence ref rejected", func(t *testing.T) {
		cp := validContextProfile()
		emptyRef := ""
		cp.WorkloadEnvelopes[0].CalibrationEvidenceRef = &emptyRef
		if err := cp.Validate(); err == nil {
			t.Fatal("expected error on empty calibration evidence ref, got nil")
		}
	})
}

func TestContextManifestValidation(t *testing.T) {
	t.Run("valid manifest passes", func(t *testing.T) {
		cm := validContextManifest()
		if err := cm.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
		if cm.RecordKind() != "ContextManifest" {
			t.Errorf("record kind: got %q, want ContextManifest", cm.RecordKind())
		}
	})

	t.Run("invalid clause digest rejected", func(t *testing.T) {
		cm := validContextManifest()
		cm.MandatoryClauses[0].ContentDigest = "md5:invalid"
		if err := cm.Validate(); err == nil {
			t.Fatal("expected error on non-sha256 clause digest, got nil")
		}
	})

	t.Run("missing mapping_version or source_revision rejected", func(t *testing.T) {
		cm := validContextManifest()
		cm.MappingVersion = ""
		if err := cm.Validate(); err == nil {
			t.Fatal("expected error on empty mapping_version, got nil")
		}

		cm = validContextManifest()
		cm.SourceRevision = ""
		if err := cm.Validate(); err == nil {
			t.Fatal("expected error on empty source_revision, got nil")
		}
	})

	t.Run("empty admission provenance rejected", func(t *testing.T) {
		cm := validContextManifest()
		cm.AdmissionProvenance = []string{}
		if err := cm.Validate(); err == nil {
			t.Fatal("expected error on empty admission_provenance, got nil")
		}
	})

	t.Run("empty string in admission provenance rejected", func(t *testing.T) {
		cm := validContextManifest()
		cm.AdmissionProvenance = []string{""}
		if err := cm.Validate(); err == nil {
			t.Fatal("expected error on empty string in admission_provenance, got nil")
		}
	})
}

func TestEvidenceLeaseValidation(t *testing.T) {
	t.Run("valid lease passes", func(t *testing.T) {
		el := validEvidenceLease()
		if err := el.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
		if el.RecordKind() != "EvidenceLease" {
			t.Errorf("record kind: got %q, want EvidenceLease", el.RecordKind())
		}
	})

	t.Run("verbatim content addressing: sha256 mismatch rejected", func(t *testing.T) {
		el := validEvidenceLease()
		el.ContentDigest = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		if err := el.Validate(); err == nil {
			t.Fatal("expected error when content digest does not match sha256 of content, got nil")
		}
	})

	t.Run("token count < 1 rejected", func(t *testing.T) {
		el := validEvidenceLease()
		el.TokenCount = 0
		if err := el.Validate(); err == nil {
			t.Fatal("expected error on token count < 1, got nil")
		}
	})
}

func TestContextPackValidation(t *testing.T) {
	t.Run("valid pack passes", func(t *testing.T) {
		pack := validContextPack()
		if err := pack.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
		if pack.RecordKind() != "ContextPack" {
			t.Errorf("record kind: got %q, want ContextPack", pack.RecordKind())
		}
	})

	t.Run("token accounting layer sum mismatch rejected", func(t *testing.T) {
		pack := validContextPack()
		pack.TokenAccounting.TotalResidentTokens = 9999
		if err := pack.Validate(); err == nil {
			t.Fatal("expected error when total resident tokens != sum of layers, got nil")
		}
	})

	t.Run("negative tokens rejected", func(t *testing.T) {
		pack := validContextPack()
		pack.TokenAccounting.RoleTokens = -1
		if err := pack.Validate(); err == nil {
			t.Fatal("expected error on negative tokens, got nil")
		}
	})

	t.Run("empty key in admitted object digests rejected", func(t *testing.T) {
		pack := validContextPack()
		pack.AdmittedObjectDigests[""] = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		if err := pack.Validate(); err == nil {
			t.Fatal("expected error on empty key in admitted_object_digests, got nil")
		}
	})

	t.Run("invalid digest in admitted object digests rejected", func(t *testing.T) {
		pack := validContextPack()
		pack.AdmittedObjectDigests["bad"] = "md5:not_a_sha256"
		if err := pack.Validate(); err == nil {
			t.Fatal("expected error on invalid digest in admitted_object_digests, got nil")
		}
	})
}
