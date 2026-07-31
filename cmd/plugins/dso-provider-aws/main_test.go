package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

func tag(k, v string) types.Tag { return types.Tag{Key: &k, Value: &v} }

// TestMergeTags_TagCannotShadowSecretData is the regression test for the tag
// shadowing finding. AWS resource tags are merged into the same key namespace
// as the secret payload as _TAG_<key>, and tags are typically writable by a
// broader IAM population than secretsmanager:GetSecretValue readers. Before
// this fix a tag simply overwrote whatever was already there, so anyone able
// to set a tag could shadow real secret material.
func TestMergeTags_TagCannotShadowSecretData(t *testing.T) {
	data := map[string]string{
		"password":      "real-secret",
		"_TAG_password": "real-secret-that-happens-to-use-the-prefix",
	}

	mergeTags(data, []types.Tag{
		// Becomes "_TAG_password" and collides with the entry above.
		tag("password", "attacker-controlled"),
	}, "db-credentials")

	if got := data["_TAG_password"]; got != "real-secret-that-happens-to-use-the-prefix" {
		t.Errorf("tag overwrote existing secret data: _TAG_password = %q", got)
	}
	if got := data["password"]; got != "real-secret" {
		t.Errorf("unprefixed secret key was modified: password = %q", got)
	}
}

// TestMergeTags_AddsNonCollidingTags confirms the fix did not break the
// feature: tags that do not collide are still attached.
func TestMergeTags_AddsNonCollidingTags(t *testing.T) {
	data := map[string]string{"password": "s3cret"}

	mergeTags(data, []types.Tag{
		tag("env", "prod"),
		tag("owner", "team-a"),
	}, "db-credentials")

	if got := data["_TAG_env"]; got != "prod" {
		t.Errorf("_TAG_env = %q, want prod", got)
	}
	if got := data["_TAG_owner"]; got != "team-a" {
		t.Errorf("_TAG_owner = %q, want team-a", got)
	}
	if got := data["password"]; got != "s3cret" {
		t.Errorf("secret data was disturbed: password = %q", got)
	}
}

// TestMergeTags_SkipsNilKeyOrValue covers the AWS SDK's pointer fields, which
// are nil-able. Dereferencing either without a check would panic the plugin.
func TestMergeTags_SkipsNilKeyOrValue(t *testing.T) {
	data := map[string]string{"password": "s3cret"}
	v := "orphan-value"
	k := "orphan-key"

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("mergeTags panicked on nil tag fields: %v", r)
		}
	}()

	mergeTags(data, []types.Tag{
		{Key: nil, Value: &v},
		{Key: &k, Value: nil},
		{Key: nil, Value: nil},
	}, "db-credentials")

	if len(data) != 1 {
		t.Errorf("expected no tags to be added, got %v", data)
	}
}

// TestMergeTags_EmptyAndNilTagList confirms the common "secret has no tags"
// path is a no-op rather than an error or panic.
func TestMergeTags_EmptyAndNilTagList(t *testing.T) {
	for name, tags := range map[string][]types.Tag{
		"nil":   nil,
		"empty": {},
	} {
		data := map[string]string{"password": "s3cret"}
		mergeTags(data, tags, "db-credentials")
		if len(data) != 1 || data["password"] != "s3cret" {
			t.Errorf("%s tag list disturbed the payload: %v", name, data)
		}
	}
}
