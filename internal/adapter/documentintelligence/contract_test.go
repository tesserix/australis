package documentintelligence_test

import (
	"testing"

	documentintelligence "github.com/tesserix/australis/internal/adapter/documentintelligence"
	mcpadapter "github.com/tesserix/australis/internal/adapter/mcp"
)

func TestExtractDocumentContractIsClosedReadOnlyAndIdentityFree(t *testing.T) {
	t.Parallel()

	contract, err := documentintelligence.ExtractDocumentContract()
	if err != nil {
		t.Fatalf("ExtractDocumentContract() error = %v", err)
	}
	if err := mcpadapter.ValidateTool(contract); err != nil {
		t.Fatalf("ValidateTool() error = %v", err)
	}
	if contract.Name != "extract_document" {
		t.Fatalf("Name = %q, want extract_document", contract.Name)
	}
}

func TestExtractDocumentFingerprintsAreStable(t *testing.T) {
	t.Parallel()

	contract, err := documentintelligence.ExtractDocumentContract()
	if err != nil {
		t.Fatalf("ExtractDocumentContract() error = %v", err)
	}
	if contract.InputFingerprint != documentintelligence.ExtractDocumentInputFingerprint {
		t.Fatalf("input fingerprint = %q", contract.InputFingerprint)
	}
	if contract.OutputFingerprint != documentintelligence.ExtractDocumentOutputFingerprint {
		t.Fatalf("output fingerprint = %q", contract.OutputFingerprint)
	}
}
