package selfhost

import (
	"context"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/protocol"
)

// The owner-local policy grants exactly one endpoint/model for the implementer
// role: another role, and an endpoint/model not in the grant, are not resolvable.
func TestOwnerLocalPolicyGrantsOnlyImplementerOnConfiguredEndpoint(t *testing.T) {
	cfg := Config{OllamaURL: "http://127.0.0.1:11434", Model: "m:1", EndpointID: "ep-a", ContextTokens: DefaultContextTokens}
	pol, err := newOwnerLocalPolicy(cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	policy, digest, err := pol.Current(context.Background())
	if err != nil || digest == "" {
		t.Fatalf("current: %v %q", err, digest)
	}
	if len(policy.Grants) != 1 || policy.Grants[0].EndpointID != "ep-a" || policy.Grants[0].ModelID != "m:1" ||
		len(policy.Grants[0].Roles) != 1 || policy.Grants[0].Roles[0] != "implementer" {
		t.Fatalf("grants = %+v", policy.Grants)
	}
	chans := []protocol.AccessChannel{}
	var bindings []protocol.RoleBinding
	for _, ep := range []string{"ep-a", "ep-b"} {
		chans = append(chans, protocol.AccessChannel{SchemaVersion: protocol.SchemaVersion1, ChannelID: "c-" + ep, EndpointID: ep,
			Kind: protocol.ChannelDirectHTTPAPI, SessionMode: protocol.SessionStatelessPerCall})
		for _, role := range []string{"implementer", "reviewer"} {
			bindings = append(bindings, protocol.RoleBinding{Role: role, EndpointID: ep, ChannelID: "c-" + ep, Priority: 1})
		}
	}
	res := execpolicy.NewPortfolioEndpointResolver(&protocol.CognitionPortfolio{PortfolioID: "p", Channels: chans, RoleBindings: bindings})
	req := execpolicy.EndpointRequest{ProjectID: "p", TaskID: "t", Role: "implementer", ContextNeed: protocol.ExposureFocusedSnippets}
	got, err := res.Resolve(context.Background(), req, policy, digest)
	if err != nil || got.EndpointID != "ep-a" || got.ModelID != "m:1" {
		t.Fatalf("implementer: %+v %v", got, err)
	}
	req.Role = "reviewer"
	if _, err := res.Resolve(context.Background(), req, policy, digest); err == nil {
		t.Error("reviewer role was granted")
	}
	// ep-b has bindings but no grant: it is never selected.
	res = execpolicy.NewPortfolioEndpointResolver(&protocol.CognitionPortfolio{PortfolioID: "p", Channels: chans[1:], RoleBindings: bindings[2:]})
	req.Role = "implementer"
	if _, err := res.Resolve(context.Background(), req, policy, digest); err == nil {
		t.Error("an ungranted endpoint was resolved")
	}
}
