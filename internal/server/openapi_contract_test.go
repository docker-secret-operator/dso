package server

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Contract test: docs/api/openapi.yaml must match the live router. It fails
// when a route is added to ServeHTTP without a spec entry, when a spec entry has
// no handler, when a spec method is rejected, or when an unspecified method is
// accepted.

type openAPIDoc struct {
	Paths map[string]map[string]struct {
		Security *[]map[string][]string `yaml:"security"`
	} `yaml:"paths"`
}

func loadSpec(t *testing.T) openAPIDoc {
	t.Helper()
	b, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var d openAPIDoc
	if err := yaml.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func concretePath(p string) string { return strings.ReplaceAll(p, "{name}", "db_password") }

func TestOpenAPI_SpecMatchesRouter(t *testing.T) {
	spec := loadSpec(t)
	s := newWebUIServer(t, actorTestToken)
	s.Config.Secrets = restServerWithSecret("db_password").Config.Secrets
	s.Reloader = restServerWithSecret("db_password").Reloader
	bearer := map[string]string{"Authorization": "Bearer " + actorTestToken, "Origin": "http://example.com"}

	for path, ops := range spec.Paths {
		for method := range ops {
			m := strings.ToUpper(method)
			w := do(s, m, concretePath(path), bearer)
			if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
				t.Errorf("spec lists %s %s but router answered %d", m, path, w.Code)
			}
		}
		if w := do(s, "DELETE", concretePath(path), bearer); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("DELETE %s = %d, want 405 (spec lists no DELETE)", path, w.Code)
		}
		if _, hasGet := ops["get"]; !hasGet {
			if w := do(s, "PUT", concretePath(path), bearer); w.Code != http.StatusMethodNotAllowed {
				t.Errorf("PUT %s = %d, want 405", path, w.Code)
			}
		}
	}
}

// Security declarations: operations that are public must say `security: []`;
// everything else must inherit the global requirement. Public = the router's
// own public set plus the webhook (own HMAC credential).
func TestOpenAPI_PublicOperationsMatchGate(t *testing.T) {
	spec := loadSpec(t)
	for path, ops := range spec.Paths {
		for method, op := range ops {
			declaredPublic := op.Security != nil && len(*op.Security) == 0
			if declaredPublic != publicRoutes[path] {
				t.Errorf("%s %s: spec public=%v, gate public=%v", strings.ToUpper(method), path, declaredPublic, publicRoutes[path])
			}
		}
	}
}

var specRouteLiteral = regexp.MustCompile(`r\.URL\.Path == "(/[^"]*)"|HasPrefix\(r\.URL\.Path, "(/[^"]*)"\)`)

func TestOpenAPI_EveryRegisteredRouteIsSpecified(t *testing.T) {
	spec := loadSpec(t)
	src, err := os.ReadFile("rest.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func (s *RESTServer) ServeHTTP")
	end := strings.Index(body[start:], "\n}\n")
	for _, m := range specRouteLiteral.FindAllStringSubmatch(body[start:start+end], -1) {
		p := m[1] + m[2]
		if p == "/api" {
			continue
		}
		if _, ok := spec.Paths[p]; ok {
			continue
		}
		// Prefix routes such as "/api/secrets/" are covered by any spec path under them.
		covered := false
		for sp := range spec.Paths {
			if strings.HasPrefix(sp, p) {
				covered = true
			}
		}
		if !covered {
			t.Errorf("route %q is registered in ServeHTTP but absent from docs/api/openapi.yaml", p)
		}
	}
}
