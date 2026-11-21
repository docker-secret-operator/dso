package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
)

type fakeKV struct {
	gotName string
	gotCtx  context.Context
	value   *string
	err     error
}

func (f *fakeKV) GetSecret(ctx context.Context, name, version string, _ *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
	f.gotName, f.gotCtx = name, ctx
	if f.err != nil {
		return azsecrets.GetSecretResponse{}, f.err
	}
	return azsecrets.GetSecretResponse{Secret: azsecrets.Secret{Value: f.value}}, nil
}

func sp(s string) *string { return &s }

func TestGetSecret_NotInitialized(t *testing.T) {
	_, err := (&AzureProvider{}).GetSecret("x")
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("got %v", err)
	}
}

func TestGetSecret_SanitisesUnderscoresToHyphens(t *testing.T) {
	f := &fakeKV{value: sp("v")}
	if _, err := (&AzureProvider{client: f}).GetSecret("db_pass_word"); err != nil {
		t.Fatal(err)
	}
	if f.gotName != "db-pass-word" {
		t.Fatalf("Azure name = %q", f.gotName)
	}
}

func TestGetSecret_PlainStringWrappedUnderValue(t *testing.T) {
	got, err := (&AzureProvider{client: &fakeKV{value: sp("hunter2")}}).GetSecret("s")
	if err != nil || len(got) != 1 || got["value"] != "hunter2" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestGetSecret_JSONObjectMapsKeys(t *testing.T) {
	got, err := (&AzureProvider{client: &fakeKV{value: sp(`{"user":"u","pass":"p"}`)}}).GetSecret("s")
	if err != nil || got["user"] != "u" || got["pass"] != "p" || len(got) != 2 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestGetSecret_NonStringJSONFallsBackToRawValue(t *testing.T) {
	raw := `{"n":1}` // not map[string]string: must not be dropped or half-decoded
	got, err := (&AzureProvider{client: &fakeKV{value: sp(raw)}}).GetSecret("s")
	if err != nil || got["value"] != raw {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestGetSecret_EmptyStructuredBodyRejected(t *testing.T) {
	for _, body := range []string{"{}", "null"} {
		_, err := (&AzureProvider{client: &fakeKV{value: sp(body)}}).GetSecret("s")
		if err == nil || !strings.Contains(err.Error(), "no key/value pairs") {
			t.Fatalf("body %q: got %v", body, err)
		}
	}
}

func TestGetSecret_NilValueRejected(t *testing.T) {
	_, err := (&AzureProvider{client: &fakeKV{}}).GetSecret("s")
	if err == nil || !strings.Contains(err.Error(), "has no value") {
		t.Fatalf("got %v", err)
	}
}

func TestGetSecret_SDKErrorWrappedWithNamesAndCause(t *testing.T) {
	cause := errors.New("404 SecretNotFound")
	_, err := (&AzureProvider{client: &fakeKV{err: cause}}).GetSecret("my_secret")
	if !errors.Is(err, cause) {
		t.Fatalf("cause not wrapped: %v", err)
	}
	if !strings.Contains(err.Error(), "'my_secret'") || !strings.Contains(err.Error(), "'my-secret'") {
		t.Fatalf("error should name both secret names: %v", err)
	}
}

func TestGetSecretWithContext_PassesCallerContextToSDK(t *testing.T) {
	type k struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), k{}, "marker"))
	cancel()
	f := &fakeKV{value: sp("v")}
	_, _ = (&AzureProvider{client: f}).GetSecretWithContext(ctx, "s")
	if f.gotCtx == nil || f.gotCtx.Value(k{}) != "marker" || f.gotCtx.Err() == nil {
		t.Fatal("caller context (with its cancellation) was not propagated to the SDK call")
	}
}

func TestInit_RequiresVaultURL(t *testing.T) {
	for _, cfg := range []map[string]string{nil, {}, {"vault_url": ""}} {
		err := (&AzureProvider{}).Init(cfg)
		if err == nil || !strings.Contains(err.Error(), "vault_url") {
			t.Fatalf("cfg %v: got %v", cfg, err)
		}
	}
}
