package actors_test

import (
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/actors"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestTypeIdentity(t *testing.T) {
	// A5: actors.ActorBasis and protocol.ActorBasis are the same type.
	var _ protocol.ActorBasis = actors.ActorBasis{}
	var _ actors.ActorBasis = protocol.ActorBasis{}
}

func TestDeriveActorID_ScenarioA1_EndpointBasis(t *testing.T) {
	basis := actors.ActorBasis{
		EndpointID:    "ep-claude-3-5",
		ModelID:       "claude-3-5-sonnet",
		ModelRevision: "20241022",
	}

	id1, err := actors.DeriveActorID(actors.BasisEndpointModel, basis)
	if err != nil {
		t.Fatalf("first derivation failed: %v", err)
	}

	id2, err := actors.DeriveActorID(actors.BasisEndpointModel, basis)
	if err != nil {
		t.Fatalf("second derivation failed: %v", err)
	}

	// Identical id when derived twice
	if id1 != id2 {
		t.Fatalf("derivations differ: %q != %q", id1, id2)
	}

	// Must start with "actor:" and have 30 characters (6 prefix + 24 hex)
	if !strings.HasPrefix(id1, "actor:") {
		t.Fatalf("id %q does not start with 'actor:'", id1)
	}
	if len(id1) != 30 {
		t.Fatalf("len(id) = %d, want 30", len(id1))
	}

	// Different model revision -> different id
	basisDiffRevision := basis
	basisDiffRevision.ModelRevision = "20241023"
	idDiffRevision, err := actors.DeriveActorID(actors.BasisEndpointModel, basisDiffRevision)
	if err != nil {
		t.Fatalf("derivation with diff revision failed: %v", err)
	}
	if idDiffRevision == id1 {
		t.Fatalf("different model revision produced same id: %q", idDiffRevision)
	}

	// Different model id -> different id
	basisDiffModel := basis
	basisDiffModel.ModelID = "claude-3-opus"
	idDiffModel, err := actors.DeriveActorID(actors.BasisEndpointModel, basisDiffModel)
	if err != nil {
		t.Fatalf("derivation with diff model failed: %v", err)
	}
	if idDiffModel == id1 {
		t.Fatalf("different model produced same id: %q", idDiffModel)
	}

	// Different endpoint id -> different id
	basisDiffEndpoint := basis
	basisDiffEndpoint.EndpointID = "ep-other"
	idDiffEndpoint, err := actors.DeriveActorID(actors.BasisEndpointModel, basisDiffEndpoint)
	if err != nil {
		t.Fatalf("derivation with diff endpoint failed: %v", err)
	}
	if idDiffEndpoint == id1 {
		t.Fatalf("different endpoint produced same id: %q", idDiffEndpoint)
	}
}

func TestDeriveActorID_ScenarioA2_MissingFieldsFailClosed(t *testing.T) {
	// Under model_family_account: missing provider, model_family or account_ref
	base := actors.ActorBasis{
		Provider:    "anthropic",
		ModelFamily: "claude-3-5",
		AccountRef:  "acc-default",
	}

	// Missing provider
	b1 := base
	b1.Provider = ""
	if _, err := actors.DeriveActorID(actors.BasisModelFamilyAccount, b1); err == nil {
		t.Fatal("missing provider under model_family_account was accepted")
	}

	// Missing model_family
	b2 := base
	b2.ModelFamily = ""
	if _, err := actors.DeriveActorID(actors.BasisModelFamilyAccount, b2); err == nil {
		t.Fatal("missing model_family under model_family_account was accepted")
	}

	// Missing account_ref
	b3 := base
	b3.AccountRef = ""
	if _, err := actors.DeriveActorID(actors.BasisModelFamilyAccount, b3); err == nil {
		t.Fatal("missing account_ref under model_family_account was accepted")
	}

	// Whitespace only
	b4 := base
	b4.AccountRef = "   "
	if _, err := actors.DeriveActorID(actors.BasisModelFamilyAccount, b4); err == nil {
		t.Fatal("whitespace-only account_ref was accepted")
	}

	// Under endpoint_model: missing endpoint_id, model_id or model_revision
	epBase := actors.ActorBasis{
		EndpointID:    "ep-1",
		ModelID:       "mod-1",
		ModelRevision: "rev-1",
	}

	ep1 := epBase
	ep1.EndpointID = ""
	if _, err := actors.DeriveActorID(actors.BasisEndpointModel, ep1); err == nil {
		t.Fatal("missing endpoint_id under endpoint_model was accepted")
	}

	ep2 := epBase
	ep2.ModelID = ""
	if _, err := actors.DeriveActorID(actors.BasisEndpointModel, ep2); err == nil {
		t.Fatal("missing model_id under endpoint_model was accepted")
	}

	ep3 := epBase
	ep3.ModelRevision = ""
	if _, err := actors.DeriveActorID(actors.BasisEndpointModel, ep3); err == nil {
		t.Fatal("missing model_revision under endpoint_model was accepted")
	}

	ep4 := epBase
	ep4.ModelID = "   "
	if _, err := actors.DeriveActorID(actors.BasisEndpointModel, ep4); err == nil {
		t.Fatal("whitespace-only model_id was accepted")
	}
}

func TestDeriveActorID_ScenarioA5_FullBasisComputableUnderBoth(t *testing.T) {
	full := actors.ActorBasis{
		EndpointID:    "ep-claude-3-5",
		ModelID:       "claude-3-5-sonnet",
		ModelRevision: "20241022",
		Provider:      "anthropic",
		ModelFamily:   "claude-3-5",
		AccountRef:    "acc-default",
	}

	idEp, err := actors.DeriveActorID(actors.BasisEndpointModel, full)
	if err != nil {
		t.Fatalf("endpoint_model derivation failed: %v", err)
	}

	idFam, err := actors.DeriveActorID(actors.BasisModelFamilyAccount, full)
	if err != nil {
		t.Fatalf("model_family_account derivation failed: %v", err)
	}

	if idEp == "" || idFam == "" {
		t.Fatal("derived IDs must not be empty")
	}
	if idEp == idFam {
		t.Fatalf("derivations under different bases should differ: %q == %q", idEp, idFam)
	}
}

func TestDeriveActorID_UnknownBasis(t *testing.T) {
	basis := actors.ActorBasis{
		EndpointID:    "ep-1",
		ModelID:       "m-1",
		ModelRevision: "r-1",
		Provider:      "p-1",
		ModelFamily:   "f-1",
		AccountRef:    "a-1",
	}

	if _, err := actors.DeriveActorID("unknown_basis", basis); err == nil {
		t.Fatal("unknown basis was accepted")
	}
	if _, err := actors.DeriveActorID("", basis); err == nil {
		t.Fatal("empty basis was accepted")
	}
}
