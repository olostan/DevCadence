package controlplane

import (
	"context"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/storage"
)

// checkProductAuthorityRefs verifies the references that carry human product
// authority before a record becomes durable.
//
// The scope is deliberately narrow. A general referential-integrity engine
// over every protocol relation would be speculative: most references point at
// artifacts later milestones produce, and M1 cannot resolve them. What M1 can
// resolve is whether a requirement claiming to rest on a ProductDecision
// actually rests on one that exists in this project — and that is the
// reference whose absence would let principal inference be recorded as
// confirmed human intent (DCI-008, DCI-009, DCI-015).
func checkProductAuthorityRefs(
	ctx context.Context, tx *storage.Tx, projectID string, record protocol.Record,
) error {
	requirement, ok := record.(*protocol.Requirement)
	if !ok {
		return nil
	}
	// Only a confirmed requirement makes a claim about human authority. A
	// proposed one citing a decision that does not exist yet is an ordinary
	// forward reference during discovery.
	if requirement.Status != protocol.RequirementConfirmed {
		return nil
	}
	if requirement.Source.Type != protocol.SourceProductDecision {
		return nil
	}
	if requirement.Source.Ref == nil || *requirement.Source.Ref == "" {
		// The typed validation already refuses this; the check is repeated
		// here so the control plane does not depend on ordering.
		return errs.New(errs.CategoryInvalidArgument,
			"requirement %s is confirmed from a product decision but names none", requirement.RequirementID)
	}
	ref := *requirement.Source.Ref
	exists, err := tx.RecordExists(ctx, projectID, "ProductDecision", ref)
	if err != nil {
		return err
	}
	if !exists {
		return errs.New(errs.CategoryIntegrity,
			"requirement %s is confirmed from product decision %s, which does not exist in project %s; "+
				"a requirement cannot claim human authority from a decision nobody recorded",
			requirement.RequirementID, ref, projectID)
	}
	return nil
}
