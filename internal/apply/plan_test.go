package apply

import (
	"context"
	"errors"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/config"
)

func cfg(providers map[string]config.ProviderConfig, secrets ...config.SecretMapping) *config.Config {
	return &config.Config{Providers: providers, Secrets: secrets}
}

func countOps(plan *ApplyPlan) map[string]int {
	m := map[string]int{}
	for _, c := range plan.Changes {
		m[c.Op+"/"+c.Kind]++
	}
	return m
}

func TestComputePlan_NilCurrent_AllCreates(t *testing.T) {
	desired := cfg(map[string]config.ProviderConfig{"vault": {Type: "vault"}},
		config.SecretMapping{Name: "db", Provider: "vault"},
		config.SecretMapping{Name: "api", Provider: "vault"},
	)
	plan := ComputePlan(nil, desired)
	if plan.TotalSecrets != 2 {
		t.Errorf("TotalSecrets = %d, want 2", plan.TotalSecrets)
	}
	if plan.SecretsToUpdate != 2 {
		t.Errorf("SecretsToUpdate = %d, want 2", plan.SecretsToUpdate)
	}
	ops := countOps(plan)
	if ops["create/secret"] != 2 || ops["create/provider"] != 1 {
		t.Errorf("unexpected ops: %#v", ops)
	}
}

func TestComputePlan_DiffCreateUpdateRemove(t *testing.T) {
	current := cfg(map[string]config.ProviderConfig{"vault": {Type: "vault"}},
		config.SecretMapping{Name: "keep", Provider: "vault"},
		config.SecretMapping{Name: "change", Provider: "vault"},
		config.SecretMapping{Name: "gone", Provider: "vault"},
	)
	desired := cfg(map[string]config.ProviderConfig{
		"vault": {Type: "vault"},
		"aws":   {Type: "aws"}, // new provider
	},
		config.SecretMapping{Name: "keep", Provider: "vault"},                    // unchanged
		config.SecretMapping{Name: "change", Provider: "aws"},                    // updated
		config.SecretMapping{Name: "new", Provider: "aws"},                       // created
	)
	plan := ComputePlan(current, desired)

	ops := countOps(plan)
	if ops["create/provider"] != 1 {
		t.Errorf("create/provider = %d, want 1", ops["create/provider"])
	}
	if ops["create/secret"] != 1 {
		t.Errorf("create/secret = %d, want 1", ops["create/secret"])
	}
	if ops["update/secret"] != 1 {
		t.Errorf("update/secret = %d, want 1", ops["update/secret"])
	}
	if ops["remove/secret"] != 1 {
		t.Errorf("remove/secret = %d, want 1", ops["remove/secret"])
	}
	// "keep" is unchanged → not a change; SecretsToUpdate counts create+update.
	if plan.SecretsToUpdate != 2 {
		t.Errorf("SecretsToUpdate = %d, want 2", plan.SecretsToUpdate)
	}
}

func TestComputePlan_NoChanges(t *testing.T) {
	c := cfg(map[string]config.ProviderConfig{"vault": {Type: "vault"}},
		config.SecretMapping{Name: "db", Provider: "vault"})
	plan := ComputePlan(c, c)
	if len(plan.Changes) != 0 {
		t.Errorf("expected no changes, got %#v", plan.Changes)
	}
	if plan.SecretsToUpdate != 0 {
		t.Errorf("SecretsToUpdate = %d, want 0", plan.SecretsToUpdate)
	}
}

func TestComputePlan_DoesNotLeakProviderConfig(t *testing.T) {
	// Provider config can hold credentials; the plan must only expose the type.
	desired := cfg(map[string]config.ProviderConfig{
		"vault": {Type: "vault", Config: map[string]string{"token": "s3cr3t"}},
	})
	plan := ComputePlan(nil, desired)
	for _, ch := range plan.Changes {
		if v, ok := ch.NewValue.(string); ok && v == "s3cr3t" {
			t.Fatal("plan leaked provider credential in NewValue")
		}
	}
}

func TestRequiresRestart(t *testing.T) {
	base := cfg(map[string]config.ProviderConfig{"vault": {Type: "vault"}},
		config.SecretMapping{Name: "db", Provider: "vault"})

	// Secret-only change → no restart.
	secretOnly := cfg(map[string]config.ProviderConfig{"vault": {Type: "vault"}},
		config.SecretMapping{Name: "db", Provider: "vault"},
		config.SecretMapping{Name: "new", Provider: "vault"})
	if RequiresRestart(base, secretOnly) {
		t.Error("secret-only change should not require restart")
	}

	// Provider change → restart.
	providerChange := cfg(map[string]config.ProviderConfig{
		"vault": {Type: "vault"}, "aws": {Type: "aws"},
	}, config.SecretMapping{Name: "db", Provider: "vault"})
	if !RequiresRestart(base, providerChange) {
		t.Error("provider change should require restart")
	}

	if RequiresRestart(nil, base) {
		t.Error("nil current should not require restart")
	}
}

type stubReconciler struct{ err error }

func (s stubReconciler) Reconcile(context.Context, *config.Config, *ApplyPlan) error {
	return s.err
}

func TestExecute(t *testing.T) {
	c := cfg(nil)
	plan := &ApplyPlan{}

	// nil reconciler → saved only.
	res, _ := Execute(context.Background(), c, plan, nil)
	if !res.Success {
		t.Error("nil reconciler should yield success")
	}

	// success
	res, _ = Execute(context.Background(), c, plan, stubReconciler{})
	if !res.Success || res.Error != "" {
		t.Errorf("expected success, got %#v", res)
	}

	// reconcile failure → success=false, error set, no hard error
	res, err := Execute(context.Background(), c, plan, stubReconciler{err: errors.New("boom")})
	if err != nil {
		t.Errorf("Execute should not return a hard error, got %v", err)
	}
	if res.Success || res.Error != "boom" {
		t.Errorf("expected failure result, got %#v", res)
	}
}
