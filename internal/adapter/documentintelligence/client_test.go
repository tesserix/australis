package documentintelligence_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	documentintelligence "github.com/tesserix/australis/internal/adapter/documentintelligence"
)

func TestClientCreatesAJobWithoutSendingIdentityOrCredentials(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ocr/jobs" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Fatal("Idempotency-Key is missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		for _, forbidden := range []string{"tenant", "product", "credential", "base_url"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("request leaked forbidden field %q: %s", forbidden, encoded)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		writeResponse(t, w, `{"job_id":"job_CREATED","status":"accepted","created_at":"2026-09-04T00:00:00Z","status_url":"/v1/ocr/jobs/job_CREATED","result_url":"/v1/ocr/jobs/job_CREATED/result"}`)
	}))
	t.Cleanup(server.Close)

	client, err := documentintelligence.NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	result, err := client.Extract(t.Context(), documentintelligence.ExtractRequest{
		UploadID:        "upl_FIXTURE",
		DocumentType:    "invoice",
		OutputFormat:    "structured",
		LanguageHints:   []string{"en"},
		IncludeEvidence: true,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.JobID != "job_CREATED" || result.Status != "accepted" {
		t.Fatalf("result = %#v", result)
	}
	if result.ContentTrust != "untrusted" {
		t.Fatalf("ContentTrust = %q", result.ContentTrust)
	}
}

func TestClientResumesACompletedJobAndMapsEvidence(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/ocr/jobs/job_READY":
			writeResponse(t, w, `{"job_id":"job_READY","status":"completed","created_at":"2026-09-04T00:00:00Z"}`)
		case "/v1/ocr/jobs/job_READY/result":
			writeResponse(t, w, `{
          "schema_version":"1.0",
          "document_id":"doc_READY",
          "document_version":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "content_trust":"untrusted",
          "fields":{
            "total":{
              "value":{"currency":"AUD","decimal":"1280.50"},
              "confidence":0.96,
              "evidence":[{
                "page":1,
                "observation_id":"obs_TOTAL",
                "polygon":{"points":[{"x":0.7,"y":0.8},{"x":0.9,"y":0.8},{"x":0.9,"y":0.85}]}
              }]
            }
          }
        }`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, err := documentintelligence.NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	result, err := client.Extract(context.Background(), documentintelligence.ExtractRequest{
		JobID:           "job_READY",
		DocumentType:    "auto",
		OutputFormat:    "structured",
		IncludeEvidence: true,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(result.Fields) != 1 || result.Fields[0].Name != "total" {
		t.Fatalf("Fields = %#v", result.Fields)
	}
	if result.Fields[0].ValueJSON != `{"currency":"AUD","decimal":"1280.50"}` {
		t.Fatalf("ValueJSON = %q", result.Fields[0].ValueJSON)
	}
	if got := result.Fields[0].Citations[0]; got.Page != 1 || len(got.Polygon) != 3 {
		t.Fatalf("citation = %#v", got)
	}
	if result.DocumentVersion != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("DocumentVersion = %q", result.DocumentVersion)
	}
}

func writeResponse(t *testing.T, writer http.ResponseWriter, body string) {
	t.Helper()
	if _, err := io.WriteString(writer, body); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func TestClientRejectsUnsafeConfigurationAndAmbiguousReferences(t *testing.T) {
	t.Parallel()

	if _, err := documentintelligence.NewClient("http://metadata.google.internal", http.DefaultClient); err == nil {
		t.Fatal("NewClient() accepted an unsafe cleartext host")
	}
	client, err := documentintelligence.NewClient("https://document-intelligence.example", http.DefaultClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Extract(context.Background(), documentintelligence.ExtractRequest{
		UploadID:        "upl_ONE",
		JobID:           "job_TWO",
		DocumentType:    "auto",
		OutputFormat:    "structured",
		IncludeEvidence: true,
	})
	if err == nil {
		t.Fatal("Extract() accepted both upload_id and job_id")
	}
}

func TestClientRejectsResultDataThatViolatesTheUntrustedEvidenceContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/result") {
			writeResponse(t, w, `{
          "schema_version":"1.0",
          "document_id":"doc_UNSAFE",
          "document_version":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "content_trust":"trusted",
          "fields":{}
        }`)
			return
		}
		writeResponse(t, w, `{"job_id":"job_UNSAFE","status":"completed"}`)
	}))
	t.Cleanup(server.Close)

	client, err := documentintelligence.NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Extract(t.Context(), documentintelligence.ExtractRequest{
		JobID:           "job_UNSAFE",
		DocumentType:    "auto",
		OutputFormat:    "structured",
		IncludeEvidence: true,
	})
	if err == nil {
		t.Fatal("Extract() accepted trusted document content")
	}
}
