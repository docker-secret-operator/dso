package provider

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/hashicorp/vault/api"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
)

// awsErr builds an error shaped like the AWS SDK v2 returns for a failed
// GetSecretValue, then wraps it the way the aws plugin does before it crosses
// the RPC boundary (text only).
func awsErr(status int, code, msg string) error {
	inner := &smithy.OperationError{
		ServiceID: "Secrets Manager", OperationName: "GetSecretValue",
		Err: &awshttp.ResponseError{
			RequestID: "req-1",
			ResponseError: &smithyhttp.ResponseError{
				Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
				Err:      &smithy.GenericAPIError{Code: code, Message: msg},
			},
		},
	}
	return fmt.Errorf("failed to fetch secret 'db-pass' from AWS Secrets Manager: %w\n  Fix: Verify the secret name and IAM permissions (secretsmanager:GetSecretValue)", inner)
}

func azureErr(t *testing.T, status int, code string, hdr map[string]string) error {
	t.Helper()
	req, _ := http.NewRequest("GET", "https://v.vault.azure.net/secrets/db-pass", nil)
	h := http.Header{"X-Ms-Error-Code": []string{code}}
	for k, v := range hdr {
		h.Set(k, v)
	}
	resp := &http.Response{StatusCode: status, Status: fmt.Sprintf("%d X", status), Header: h, Request: req, Body: io.NopCloser(strings.NewReader(""))}
	return fmt.Errorf("failed to fetch secret 'db-pass' (Azure name: 'db-pass') from Azure Key Vault: %w\n  Fix: x", runtime.NewResponseError(resp))
}

func vaultErr(status int, msg string) error {
	return fmt.Errorf("failed to read vault secret db-pass: %w", &api.ResponseError{
		HTTPMethod: "GET", URL: "https://vault:8200/v1/secret/data/db-pass", StatusCode: status, Errors: []string{msg},
	})
}

func huaweiErr(status int, code, msg string) error {
	return fmt.Errorf("failed to fetch secret 'db-pass' from Huawei CSMS: %w\n  Fix: x",
		sdkerr.ServiceResponseError{StatusCode: status, RequestId: "r", ErrorCode: code, ErrorMessage: msg})
}

type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "dial tcp: i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }

var _ net.Error = timeoutNetErr{}

func TestClassifyTable(t *testing.T) {
	flat := func(e error) error { return rpc.ServerError(e.Error()) } // what survives net/rpc
	cases := []struct {
		name string
		err  error
		want ErrClass
	}{
		// AWS
		{"aws throttling 400", awsErr(400, "ThrottlingException", "Rate exceeded"), ClassThrottled},
		{"aws 429", awsErr(429, "TooManyRequestsException", "slow"), ClassThrottled},
		{"aws 500", awsErr(500, "InternalServiceError", "oops"), ClassTransient},
		{"aws 503", awsErr(503, "ServiceUnavailable", "oops"), ClassTransient},
		{"aws not found", awsErr(400, "ResourceNotFoundException", "no such secret"), ClassPermanent},
		{"aws access denied", awsErr(400, "AccessDeniedException", "denied"), ClassPermanent},
		{"aws bad creds 403", awsErr(403, "UnrecognizedClientException", "bad token"), ClassPermanent},
		{"aws expired token", awsErr(400, "ExpiredTokenException", "expired"), ClassPermanent},
		// Azure
		{"azure 404", azureErr(t, 404, "SecretNotFound", nil), ClassPermanent},
		{"azure 403", azureErr(t, 403, "Forbidden", nil), ClassPermanent},
		{"azure 401", azureErr(t, 401, "Unauthorized", nil), ClassPermanent},
		{"azure 429", azureErr(t, 429, "Throttled", map[string]string{"Retry-After": "7"}), ClassThrottled},
		{"azure 503", azureErr(t, 503, "ServiceUnavailable", nil), ClassTransient},
		// Vault
		{"vault 403", vaultErr(403, "permission denied"), ClassPermanent},
		{"vault 404 not found", errors.New("vault secret db-pass not found"), ClassPermanent},
		{"vault 429", vaultErr(429, "rate limit quota exceeded"), ClassThrottled},
		{"vault 503 sealed", vaultErr(503, "Vault is sealed"), ClassTransient},
		{"vault 500", vaultErr(500, "internal error"), ClassTransient},
		{"vault bad format", errors.New("invalid vault secret format for db-pass"), ClassPermanent},
		// Huawei
		{"huawei 404", huaweiErr(404, "CSMS.0404", "not exist"), ClassPermanent},
		{"huawei 401", huaweiErr(401, "APIGW.0301", "bad auth"), ClassPermanent},
		{"huawei 429", huaweiErr(429, "APIGW.0308", "flow control"), ClassThrottled},
		{"huawei 500", huaweiErr(500, "CSMS.0500", "internal"), ClassTransient},
		{"huawei connection", fmt.Errorf("x: %w", sdkerr.NewConnectionError("dial tcp 1.2.3.4:443: connection refused")), ClassTransient},
		{"huawei request timeout", fmt.Errorf("x: %w", sdkerr.NewRequestTimeoutError("net/http: request canceled (Client.Timeout exceeded)")), ClassTransient},
		// network / daemon-local
		{"net timeout", timeoutNetErr{}, ClassTransient},
		{"conn reset", errors.New("read tcp: connection reset by peer"), ClassTransient},
		{"conn refused", errors.New("dial tcp 127.0.0.1:1: connect: connection refused"), ClassTransient},
		{"temp dns", errors.New("dial tcp: lookup x: Temporary failure in name resolution"), ClassTransient},
		{"unexpected eof", io.ErrUnexpectedEOF, ClassTransient},
		{"deadline", fmt.Errorf("w: %w", errDeadline), ClassTransient},
		{"plugin gone", rpc.ErrShutdown, ClassPermanent},
		{"not initialized", errors.New("aws provider not initialized — Init() was not called"), ClassPermanent},
		{"unknown shape", errors.New("something weird happened"), ClassPermanent},
		{"unsupported", errors.New("operation unsupported by provider"), ClassPermanent},
		{"empty secret", errors.New("AWS secret 'x' decoded to no key/value pairs"), ClassPermanent},
	}
	for _, c := range cases {
		for _, form := range []struct {
			n string
			e error
		}{{"typed", c.err}, {"rpc-flattened", flat(c.err)}} {
			t.Run(c.name+"/"+form.n, func(t *testing.T) {
				// typed-only checks (context/net.Error/ErrShutdown) are
				// meaningless once flattened to a string.
				if form.n == "rpc-flattened" && (c.name == "net timeout" || c.name == "plugin gone" || c.name == "unexpected eof" || c.name == "deadline") {
					t.Skip("typed-only shape; text form checked separately")
				}
				got := Classify(form.e, "db-pass")
				if got.Class != c.want {
					t.Fatalf("Classify(%q) = %s, want %s", form.e, got.Class, c.want)
				}
			})
		}
	}
}

func TestClassifyRetryAfter(t *testing.T) {
	// Azure's ResponseError text does NOT include headers, so Retry-After is
	// not recoverable there: the classification is throttled with no hint and
	// the wrapper falls back to its floor delay.
	got := Classify(azureErr(t, 429, "Throttled", map[string]string{"Retry-After": "7"}), "db-pass")
	if got.Class != ClassThrottled || got.RetryAfter != 0 {
		t.Fatalf("azure: got %+v", got)
	}
	// Where the text does carry the header (proxy / future SDK), honour it.
	got = Classify(errors.New("StatusCode: 429, Retry-After: 7, throttled"), "k")
	if got.Class != ClassThrottled || got.RetryAfter != 7*time.Second {
		t.Fatalf("text: got %+v", got)
	}
}

func TestClassifyIgnoresSecretNameWords(t *testing.T) {
	// A key named after a transient marker must not turn "not found" transient.
	e := errors.New("vault secret timeout-token not found")
	if got := Classify(e, "timeout-token"); got.Class != ClassPermanent {
		t.Fatalf("got %s", got.Class)
	}
	// And a key containing a throttle word must not make a plain failure throttled.
	e = errors.New("failed to fetch secret 'throttling-key': something weird")
	if got := Classify(e, "throttling-key"); got.Class != ClassPermanent {
		t.Fatalf("got %s", got.Class)
	}
}

var errDeadline = errContextDeadline()
