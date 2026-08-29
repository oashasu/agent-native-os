package authz

import (
	"github.com/example/agent-native-microkernel/internal/manifest"
	"testing"
)

func TestDeclarationAndGrantRequired(t *testing.T) {
	e := New()
	m := manifest.Manifest{Plugin: manifest.Plugin{ID: "consumer"}, Consumes: manifest.Consumes{Required: []manifest.Capability{{Name: "demo.echo", Major: 1}}}}
	if err := e.CanConsume(m, "demo.echo", 1); err == nil {
		t.Fatal("expected denial without host grant")
	}
	e.SetGrant("consumer", Grant{Capabilities: []string{"demo.echo@1"}})
	if err := e.CanConsume(m, "demo.echo", 1); err != nil {
		t.Fatal(err)
	}
	if err := e.CanConsume(m, "demo.secret", 1); err == nil {
		t.Fatal("expected undeclared denial")
	}
}

func TestDelegationScopeIsExplicitAndRootScoped(t *testing.T) {
	e := New()
	e.SetGrant("alice", Grant{Capabilities: []string{"workflow.run@1"}, Delegations: map[string][]string{"workflow.run@1": {"work.*@1"}}})
	s := e.DelegationScope("alice", "workflow.run", 1)
	if !ScopeAllows(s, "work.create", 1) {
		t.Fatal("delegated child capability missing")
	}
	if ScopeAllows(s, "deploy.production", 1) {
		t.Fatal("delegation scope widened unexpectedly")
	}
	if got := e.DelegationScope("alice", "other.run", 1); len(got) != 0 {
		t.Fatalf("delegation leaked to another root capability: %v", got)
	}
}
