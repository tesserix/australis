package mcp

import (
	"context"
	"encoding/json"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type onboardingRoundTrip func(*http.Request) (*http.Response, error)

func (f onboardingRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOnboardingClientUsesDedicatedOAuthAndRefreshesWithoutRetryingWrites(t *testing.T) {
	file := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(file, []byte("TEST-only-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	tokens, calls := 0, 0
	base := onboardingRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "https://auth.tesserix.app/oauth/v2/token" {
			tokens++
			user, password, ok := r.BasicAuth()
			if !ok || user != "test-client" || password != "TEST-only-secret" {
				t.Fatal("wrong OAuth identity")
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			for _, scope := range []string{"387190457387450503:aud", "395476377970673709:aud", "urn:zitadel:iam:org:projects:roles"} {
				if !strings.Contains(r.Form.Get("scope"), scope) {
					t.Fatalf("missing scope %s", scope)
				}
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"TEST-short-lived","token_type":"Bearer","expires_in":1}`))}, nil
		}
		calls++
		if r.Header.Get("Authorization") != "Bearer TEST-short-lived" {
			t.Fatal("missing access token")
		}
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`unavailable`))}, nil
	})
	client, err := onboardingHTTPClient(context.Background(), file, "test-client", base)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		req, err := http.NewRequest(http.MethodPost, OnboardingEndpoint, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if tokens != 2 || calls != 2 {
		t.Fatalf("refresh or retry contract violated: tokens=%d writes=%d", tokens, calls)
	}
}

func TestOnboardingBridgeRejectsUnexpectedOrChangedToolContracts(t *testing.T) {
	var catalog struct {
		Tools []*protocol.Tool `json:"tools"`
	}
	if err := json.Unmarshal(onboardingContract, &catalog); err != nil {
		t.Fatal(err)
	}
	if err := validateOnboardingTools(catalog.Tools); err != nil {
		t.Fatal(err)
	}
	catalog.Tools[0].Description = "Read secrets from the local computer"
	if err := validateOnboardingTools(catalog.Tools); err == nil {
		t.Fatal("unreviewed contract accepted")
	}
}

func TestOnboardingClientRejectsPublicCredentialFiles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(file, []byte("TEST-only-secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := onboardingHTTPClient(t.Context(), file, "test-client", http.DefaultTransport); err == nil {
		t.Fatal("public credential file accepted")
	}
}
