package actors

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ActorBasis aliases protocol.ActorBasis (EWP WP-M5-R2 Part A).
type ActorBasis = protocol.ActorBasis

const (
	// BasisEndpointModel derives an actor from {endpoint_id, model_id, model_revision}.
	BasisEndpointModel = "endpoint_model"
	// BasisModelFamilyAccount derives an actor from {provider, model_family, account_ref}.
	BasisModelFamilyAccount = "model_family_account"
)

// DeriveActorID deterministically derives a stable actor identifier from
// an actor basis under the specified basis strategy (EWP WP-M5-R2 Part A).
//
// Basis "endpoint_model": selects {b.EndpointID, b.ModelID, b.ModelRevision}.
// Basis "model_family_account": selects {b.Provider, b.ModelFamily, b.AccountRef}.
// Returns "actor:" + first 24 hex of sha256 of canonical JSON of selected fields.
// Fails closed if any selected field is empty or if basis is unknown.
func DeriveActorID(basis string, b ActorBasis) (string, error) {
	var payload any
	switch basis {
	case BasisEndpointModel:
		if strings.TrimSpace(b.EndpointID) == "" ||
			strings.TrimSpace(b.ModelID) == "" ||
			strings.TrimSpace(b.ModelRevision) == "" {
			return "", errs.New(errs.CategoryInvalidArgument,
				"actors.DeriveActorID: %s requires non-empty endpoint_id, model_id, model_revision", basis)
		}
		payload = map[string]string{
			"endpoint_id":    b.EndpointID,
			"model_id":       b.ModelID,
			"model_revision": b.ModelRevision,
		}
	case BasisModelFamilyAccount:
		if strings.TrimSpace(b.Provider) == "" ||
			strings.TrimSpace(b.ModelFamily) == "" ||
			strings.TrimSpace(b.AccountRef) == "" {
			return "", errs.New(errs.CategoryInvalidArgument,
				"actors.DeriveActorID: %s requires non-empty provider, model_family, account_ref", basis)
		}
		payload = map[string]string{
			"account_ref":  b.AccountRef,
			"model_family": b.ModelFamily,
			"provider":     b.Provider,
		}
	default:
		return "", errs.New(errs.CategoryInvalidArgument,
			"actors.DeriveActorID: unknown basis %q", basis)
	}

	canonical, err := protocol.CanonicalJSON(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "actor:" + hex.EncodeToString(sum[:])[:24], nil
}
