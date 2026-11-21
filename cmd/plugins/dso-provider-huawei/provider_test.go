package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/csms/v1/model"
)

type fakeCSMS struct {
	calls  int
	gotReq *model.ShowSecretVersionRequest
	resp   *model.ShowSecretVersionResponse
	err    error
}

func (f *fakeCSMS) ShowSecretVersion(r *model.ShowSecretVersionRequest) (*model.ShowSecretVersionResponse, error) {
	f.calls++
	f.gotReq = r
	return f.resp, f.err
}

func respWith(s *string) *model.ShowSecretVersionResponse {
	return &model.ShowSecretVersionResponse{Version: &model.Version{SecretString: s}}
}

func sp(s string) *string { return &s }

func TestGetSecret_NotInitialized(t *testing.T) {
	_, err := (&HuaweiProvider{}).GetSecret("x")
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("got %v", err)
	}
}

func TestGetSecret_RequestsLatestVersionByExactName(t *testing.T) {
	f := &fakeCSMS{resp: respWith(sp("v"))}
	if _, err := (&HuaweiProvider{client: f}).GetSecret("db_pass"); err != nil {
		t.Fatal(err)
	}
	// unlike Azure, names are NOT rewritten
	if f.gotReq.SecretName != "db_pass" || f.gotReq.VersionId != "latest" {
		t.Fatalf("req = %+v", f.gotReq)
	}
}

func TestGetSecret_PlainStringWrappedUnderValue(t *testing.T) {
	got, err := (&HuaweiProvider{client: &fakeCSMS{resp: respWith(sp("hunter2"))}}).GetSecret("s")
	if err != nil || len(got) != 1 || got["value"] != "hunter2" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestGetSecret_JSONObjectMapsKeys(t *testing.T) {
	got, err := (&HuaweiProvider{client: &fakeCSMS{resp: respWith(sp(`{"a":"1","b":"2"}`))}}).GetSecret("s")
	if err != nil || got["a"] != "1" || got["b"] != "2" || len(got) != 2 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestGetSecret_EmptyStructuredBodyRejected(t *testing.T) {
	for _, body := range []string{"{}", "null"} {
		_, err := (&HuaweiProvider{client: &fakeCSMS{resp: respWith(sp(body))}}).GetSecret("s")
		if err == nil || !strings.Contains(err.Error(), "no key/value pairs") {
			t.Fatalf("body %q: got %v", body, err)
		}
	}
}

func TestGetSecret_MissingVersionOrStringRejected(t *testing.T) {
	for name, r := range map[string]*model.ShowSecretVersionResponse{
		"nil version":       {},
		"nil secret string": respWith(nil),
	} {
		_, err := (&HuaweiProvider{client: &fakeCSMS{resp: r}}).GetSecret("s")
		if err == nil || !strings.Contains(err.Error(), "empty secret") {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}

func TestGetSecret_SDKErrorWrapped(t *testing.T) {
	cause := errors.New("CSMS.0001 not found")
	_, err := (&HuaweiProvider{client: &fakeCSMS{err: cause}}).GetSecret("s")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "'s'") {
		t.Fatalf("got %v", err)
	}
}

// The SDK call takes no ctx, so the only honoured behaviour is the pre-flight
// check: an already-cancelled context must not reach the SDK at all.
func TestGetSecretWithContext_CancelledContextSkipsSDKCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeCSMS{resp: respWith(sp("v"))}
	_, err := (&HuaweiProvider{client: f}).GetSecretWithContext(ctx, "s")
	if !errors.Is(err, context.Canceled) || f.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, f.calls)
	}
}

func TestInit_InvalidRegionRejected(t *testing.T) {
	err := (&HuaweiProvider{}).Init(map[string]string{"access_key": "ak", "secret_key": "sk", "region": "mars-1"})
	if err == nil || !strings.Contains(err.Error(), "invalid Huawei Cloud region") {
		t.Fatalf("got %v", err)
	}
}

func TestInit_ConfigCredentialsAndDefaultRegionBuildClient(t *testing.T) {
	t.Setenv("HUAWEI_REGION", "")
	h := &HuaweiProvider{}
	if err := h.Init(map[string]string{"access_key": "ak", "secret_key": "sk", "project_id": "p"}); err != nil {
		t.Skipf("SDK refused offline build: %v", err)
	}
	if h.client == nil {
		t.Fatal("client not set after successful Init")
	}
}
