package protocol_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

func validAccessChannel() *protocol.AccessChannel {
	credID := "cred_oauth_01"
	return &protocol.AccessChannel{
		SchemaVersion:         protocol.SchemaVersion1,
		ChannelID:             "chan_1",
		EndpointID:            "ep_1",
		Kind:                  protocol.ChannelCLISubprocess,
		SessionMode:           protocol.SessionResumableHandle,
		ContextControl:        protocol.ContextControlAppendOnly,
		PrefixCache:           protocol.PrefixCacheImplicit,
		SupportsStreaming:     true,
		SupportsTools:         true,
		NativeWorktreeAccess:  true,
		CredentialRefID:       &credID,
		MaxConcurrentRequests: 2,
	}
}

func TestAccessChannelValidation(t *testing.T) {
	t.Run("valid channel passes", func(t *testing.T) {
		ch := validAccessChannel()
		if err := ch.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
		if ch.RecordKind() != "AccessChannel" {
			t.Errorf("record kind: got %q, want AccessChannel", ch.RecordKind())
		}
		if ch.RecordID() != "chan_1" {
			t.Errorf("record ID: got %q, want chan_1", ch.RecordID())
		}
		if ch.SchemaVer() != protocol.SchemaVersion1 {
			t.Errorf("schema ver: got %q, want %q", ch.SchemaVer(), protocol.SchemaVersion1)
		}
	})

	t.Run("empty channel_id is rejected", func(t *testing.T) {
		ch := validAccessChannel()
		ch.ChannelID = ""
		if err := ch.Validate(); err == nil {
			t.Fatal("expected error on empty channel_id, got nil")
		}
	})

	t.Run("empty endpoint_id is rejected", func(t *testing.T) {
		ch := validAccessChannel()
		ch.EndpointID = ""
		if err := ch.Validate(); err == nil {
			t.Fatal("expected error on empty endpoint_id, got nil")
		}
	})

	t.Run("invalid kind is rejected", func(t *testing.T) {
		ch := validAccessChannel()
		ch.Kind = "carrier_pigeon"
		if err := ch.Validate(); err == nil {
			t.Fatal("expected error on invalid kind, got nil")
		}
	})

	t.Run("invalid session mode is rejected", func(t *testing.T) {
		ch := validAccessChannel()
		ch.SessionMode = "infinite_state"
		if err := ch.Validate(); err == nil {
			t.Fatal("expected error on invalid session_mode, got nil")
		}
	})

	t.Run("invalid context control is rejected", func(t *testing.T) {
		ch := validAccessChannel()
		ch.ContextControl = "dynamic_rag"
		if err := ch.Validate(); err == nil {
			t.Fatal("expected error on invalid context_control, got nil")
		}
	})

	t.Run("invalid prefix cache is rejected", func(t *testing.T) {
		ch := validAccessChannel()
		ch.PrefixCache = "quantum_cache"
		if err := ch.Validate(); err == nil {
			t.Fatal("expected error on invalid prefix_cache, got nil")
		}
	})

	t.Run("max_concurrent_requests < 1 is rejected", func(t *testing.T) {
		ch := validAccessChannel()
		ch.MaxConcurrentRequests = 0
		if err := ch.Validate(); err == nil {
			t.Fatal("expected error on max_concurrent_requests < 1, got nil")
		}
	})

	t.Run("empty string credential_ref_id pointer is rejected", func(t *testing.T) {
		ch := validAccessChannel()
		empty := ""
		ch.CredentialRefID = &empty
		if err := ch.Validate(); err == nil {
			t.Fatal("expected error on empty string credential_ref_id pointer, got nil")
		}
	})
}
