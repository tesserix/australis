package documentintelligence

import (
	"encoding/json"
	"fmt"

	mcpadapter "github.com/tesserix/australis/internal/adapter/mcp"
)

const (
	ExtractDocumentInputFingerprint  = "adafd5a6492ce55b11b39a759541b65f9bd7292b69525b871474ddb612a79bee"
	ExtractDocumentOutputFingerprint = "5224dfad36d8a1e99df734db7d1903baa58329d724bc50a7f830b2d96439d015"
)

var extractDocumentInputSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "upload_id":{"type":"string","pattern":"^upl_[A-Za-z0-9_]{1,64}$"},
    "job_id":{"type":"string","pattern":"^job_[A-Za-z0-9_]{1,64}$"},
    "document_type":{"type":"string","enum":["auto","general","invoice","receipt","purchase_order","identity_document","contract","bank_statement","medical_form","application_form","resume"]},
    "output_format":{"type":"string","enum":["structured","text","markdown"]},
    "schema":{"type":"object","additionalProperties":false,"properties":{"schema_id":{"type":"string","minLength":1,"maxLength":128},"schema_version":{"type":"string","minLength":1,"maxLength":64}},"required":["schema_id","schema_version"]},
    "language_hints":{"type":"array","maxItems":8,"items":{"type":"string","minLength":2,"maxLength":35}},
    "include_evidence":{"type":"boolean"}
  },
  "required":["document_type","output_format","include_evidence"],
  "oneOf":[
    {"required":["upload_id"],"not":{"required":["job_id"]}},
    {"required":["job_id"],"not":{"required":["upload_id"]}}
  ]
}`)

var extractDocumentOutputSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "job_id":{"type":"string","pattern":"^job_[A-Za-z0-9_]{1,64}$"},
    "status":{"type":"string","enum":["accepted","inspecting","processing","validating","cancelling","cancelled","rejected","partial","review_required","completed"]},
    "content_trust":{"type":"string","const":"untrusted"},
    "result_schema_version":{"type":"string"},
    "document_id":{"type":"string","pattern":"^doc_[A-Za-z0-9_]{1,64}$"},
    "document_version":{"type":"string","pattern":"^sha256:[a-f0-9]{64}$"},
    "text":{"type":"string"},
    "markdown":{"type":"string"},
    "pages":{"type":"array","maxItems":300,"items":{"type":"object","additionalProperties":false,"properties":{"page":{"type":"integer","minimum":1},"width":{"type":"integer","minimum":1},"height":{"type":"integer","minimum":1},"observations":{"type":"array","maxItems":100000,"items":{"$ref":"#/$defs/text_observation"}}},"required":["page","width","height","observations"]}},
    "fields":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"value_json":{"type":"string"},"confidence":{"type":"number","minimum":0,"maximum":1},"citations":{"type":"array","items":{"$ref":"#/$defs/citation"}}},"required":["name","value_json","confidence","citations"]}},
    "tables":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"table_id":{"type":"string"},"cells":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"row":{"type":"integer","minimum":0},"column":{"type":"integer","minimum":0},"text":{"type":"string"},"confidence":{"type":"number","minimum":0,"maximum":1},"citations":{"type":"array","items":{"$ref":"#/$defs/citation"}}},"required":["row","column","text","confidence","citations"]}}},"required":["table_id","cells"]}},
    "confidence":{"type":"object","additionalProperties":false,"properties":{"input_quality":{"type":"number","minimum":0,"maximum":1},"ocr":{"type":"number","minimum":0,"maximum":1},"classification":{"type":"number","minimum":0,"maximum":1},"extraction":{"type":"number","minimum":0,"maximum":1},"validation":{"type":"number","minimum":0,"maximum":1},"overall":{"type":"number","minimum":0,"maximum":1}},"required":["input_quality","ocr","classification","extraction","validation","overall"]},
    "citations":{"type":"array","items":{"$ref":"#/$defs/citation"}},
    "warnings":{"type":"array","items":{"type":"string"}},
    "validation_failures":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"code":{"type":"string"},"severity":{"type":"string","enum":["warning","error"]}},"required":["code","severity"]}},
    "provider":{"type":"string"},
    "model_version":{"type":"string"},
    "processing_profile_version":{"type":"string"},
    "duration_ms":{"type":"integer","minimum":0},
    "cost":{"type":"object","additionalProperties":false,"properties":{"currency":{"type":"string","pattern":"^[A-Z]{3}$"},"decimal":{"type":"string","pattern":"^[0-9]+(\\.[0-9]+)?$"}},"required":["currency","decimal"]}
  },
  "required":["job_id","status","content_trust","warnings","validation_failures"],
  "$defs":{
    "citation":{"type":"object","additionalProperties":false,"properties":{"document_version":{"type":"string","pattern":"^sha256:[a-f0-9]{64}$"},"page":{"type":"integer","minimum":1},"polygon":{"type":"array","minItems":3,"items":{"type":"array","minItems":2,"maxItems":2,"items":{"type":"number","minimum":0,"maximum":1}}},"observation_id":{"type":"string","pattern":"^obs_[A-Za-z0-9_]{1,64}$"}},"required":["document_version","page","polygon","observation_id"]},
    "text_observation":{"type":"object","additionalProperties":false,"properties":{"observation_id":{"type":"string","pattern":"^obs_[A-Za-z0-9_]{1,64}$"},"level":{"type":"string","enum":["page","paragraph","line","word"]},"text":{"type":"string","minLength":1,"maxLength":65536},"confidence":{"type":"number","minimum":0,"maximum":1},"polygon":{"type":"array","minItems":3,"items":{"type":"array","minItems":2,"maxItems":2,"items":{"type":"number","minimum":0,"maximum":1}}},"reading_order":{"type":"integer","minimum":0},"parent_observation_id":{"type":"string","pattern":"^obs_[A-Za-z0-9_]{1,64}$"}},"required":["observation_id","level","text","confidence","polygon","reading_order"]}
  }
}`)

func ExtractDocumentContract() (mcpadapter.ToolContract, error) {
	inputFingerprint, err := mcpadapter.FingerprintSchema(extractDocumentInputSchema)
	if err != nil {
		return mcpadapter.ToolContract{}, fmt.Errorf("fingerprint extract_document input: %w", err)
	}
	outputFingerprint, err := mcpadapter.FingerprintSchema(extractDocumentOutputSchema)
	if err != nil {
		return mcpadapter.ToolContract{}, fmt.Errorf("fingerprint extract_document output: %w", err)
	}
	return mcpadapter.ToolContract{
		Name:              "extract_document",
		InputSchema:       extractDocumentInputSchema,
		OutputSchema:      extractDocumentOutputSchema,
		InputFingerprint:  inputFingerprint,
		OutputFingerprint: outputFingerprint,
		DirectAccess:      false,
	}, nil
}
