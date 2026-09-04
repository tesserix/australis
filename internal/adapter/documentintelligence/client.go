package documentintelligence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	requestTimeout     = 10 * time.Second
	maximumResponseLen = 16 * 1024 * 1024
)

var (
	ErrInvalidRequest  = errors.New("invalid extract document request")
	ErrNotFound        = errors.New("document job not found")
	ErrUnavailable     = errors.New("document intelligence unavailable")
	uploadIDPattern    = regexp.MustCompile(`^upl_[A-Za-z0-9_]{1,64}$`)
	jobIDPattern       = regexp.MustCompile(`^job_[A-Za-z0-9_]{1,64}$`)
	documentIDPattern  = regexp.MustCompile(`^doc_[A-Za-z0-9_]{1,64}$`)
	observationPattern = regexp.MustCompile(`^obs_[A-Za-z0-9_]{1,64}$`)
	tableIDPattern     = regexp.MustCompile(`^tbl_[A-Za-z0-9_]{1,64}$`)
	digestPattern      = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	codePattern        = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	versionPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	currencyPattern    = regexp.MustCompile(`^[A-Z]{3}$`)
	decimalPattern     = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

type SchemaReference struct {
	SchemaID      string `json:"schema_id"`
	SchemaVersion string `json:"schema_version"`
}

type ExtractRequest struct {
	UploadID        string
	JobID           string
	DocumentType    string
	OutputFormat    string
	Schema          *SchemaReference
	LanguageHints   []string
	IncludeEvidence bool
}

type Citation struct {
	DocumentVersion string      `json:"document_version"`
	Page            uint32      `json:"page"`
	Polygon         [][]float64 `json:"polygon"`
	ObservationID   string      `json:"observation_id"`
}

type Field struct {
	Name       string     `json:"name"`
	ValueJSON  string     `json:"value_json"`
	Confidence float64    `json:"confidence"`
	Citations  []Citation `json:"citations"`
}

type ValidationFailure struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
}

type TableCell struct {
	Row        uint32     `json:"row"`
	Column     uint32     `json:"column"`
	Text       string     `json:"text"`
	Confidence float64    `json:"confidence"`
	Citations  []Citation `json:"citations"`
}

type Table struct {
	TableID string      `json:"table_id"`
	Cells   []TableCell `json:"cells"`
}

type TextObservation struct {
	ObservationID       string      `json:"observation_id"`
	Level               string      `json:"level"`
	Text                string      `json:"text"`
	Confidence          float64     `json:"confidence"`
	Polygon             [][]float64 `json:"polygon"`
	ReadingOrder        uint32      `json:"reading_order"`
	ParentObservationID *string     `json:"parent_observation_id,omitempty"`
}

type DocumentPage struct {
	Page         uint32            `json:"page"`
	Width        uint32            `json:"width"`
	Height       uint32            `json:"height"`
	Observations []TextObservation `json:"observations"`
}

type Confidence struct {
	InputQuality   float64 `json:"input_quality"`
	OCR            float64 `json:"ocr"`
	Classification float64 `json:"classification"`
	Extraction     float64 `json:"extraction"`
	Validation     float64 `json:"validation"`
	Overall        float64 `json:"overall"`
}

type Cost struct {
	Currency string `json:"currency"`
	Decimal  string `json:"decimal"`
}

type ExtractResponse struct {
	JobID               string              `json:"job_id"`
	Status              string              `json:"status"`
	ContentTrust        string              `json:"content_trust"`
	ResultSchemaVersion string              `json:"result_schema_version,omitempty"`
	DocumentID          string              `json:"document_id,omitempty"`
	DocumentVersion     string              `json:"document_version,omitempty"`
	Text                string              `json:"text,omitempty"`
	Markdown            string              `json:"markdown,omitempty"`
	Pages               []DocumentPage      `json:"pages,omitempty"`
	Fields              []Field             `json:"fields,omitempty"`
	Tables              []Table             `json:"tables,omitempty"`
	Confidence          *Confidence         `json:"confidence,omitempty"`
	Citations           []Citation          `json:"citations,omitempty"`
	Warnings            []string            `json:"warnings"`
	ValidationFailures  []ValidationFailure `json:"validation_failures"`
	Provider            string              `json:"provider,omitempty"`
	ModelVersion        string              `json:"model_version,omitempty"`
	ProcessingProfile   string              `json:"processing_profile_version,omitempty"`
	DurationMS          uint64              `json:"duration_ms,omitempty"`
	Cost                *Cost               `json:"cost,omitempty"`
}

type apiError struct {
	StatusCode int
	Code       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("document intelligence returned status %d with code %s", e.StatusCode, e.Code)
}

func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("http client is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("document intelligence base URL is invalid")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("document intelligence base URL must not contain a path")
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !isLocalHTTPHost(parsed.Hostname())) {
		return nil, fmt.Errorf("document intelligence base URL must use HTTPS")
	}
	parsed.Path = ""
	ownedClient := *httpClient
	ownedClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{baseURL: parsed, httpClient: &ownedClient}, nil
}

func (c *Client) Extract(ctx context.Context, request ExtractRequest) (ExtractResponse, error) {
	if err := validateExtractRequest(request); err != nil {
		return ExtractResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if request.UploadID != "" {
		return c.create(ctx, request)
	}
	return c.resume(ctx, request.JobID)
}

func (c *Client) create(ctx context.Context, request ExtractRequest) (ExtractResponse, error) {
	payload := createJobRequest{
		Source:        source{UploadID: request.UploadID},
		DocumentType:  request.DocumentType,
		Output:        outputOptionsFor(request.OutputFormat, request.IncludeEvidence),
		Extraction:    request.Schema,
		LanguageHints: request.LanguageHints,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ExtractResponse{}, fmt.Errorf("encode create job request: %w", err)
	}
	digest := sha256.Sum256(body)
	headers := http.Header{
		"Content-Type":    []string{"application/json"},
		"Idempotency-Key": []string{"australis-extract-" + hex.EncodeToString(digest[:])},
	}
	var response jobResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/ocr/jobs", body, headers, &response); err != nil {
		return ExtractResponse{}, err
	}
	if err := validateJobResponse(response); err != nil {
		return ExtractResponse{}, err
	}
	return pendingResponse(response.JobID, response.Status), nil
}

func (c *Client) resume(ctx context.Context, jobID string) (ExtractResponse, error) {
	var status jobResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/ocr/jobs/"+url.PathEscape(jobID), nil, nil, &status); err != nil {
		return ExtractResponse{}, err
	}
	if err := validateJobResponse(status); err != nil {
		return ExtractResponse{}, err
	}
	if !resultAvailable(status.Status) {
		return pendingResponse(status.JobID, status.Status), nil
	}
	var result documentResult
	path := "/v1/ocr/jobs/" + url.PathEscape(jobID) + "/result"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &result); err != nil {
		return ExtractResponse{}, err
	}
	return mapResult(status, result)
}

func (c *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	body []byte,
	headers http.Header,
	target any,
) error {
	endpoint := c.baseURL.JoinPath(path)
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create document intelligence request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	for name, values := range headers {
		request.Header[name] = append([]string(nil), values...)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call document intelligence: %w", err)
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maximumResponseLen+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read document intelligence response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close document intelligence response: %w", closeErr)
	}
	if len(responseBody) > maximumResponseLen {
		return fmt.Errorf("document intelligence response exceeds limit")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return decodeAPIError(response.StatusCode, responseBody)
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode document intelligence response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode document intelligence response: trailing content")
	}
	return nil
}

func decodeAPIError(statusCode int, body []byte) error {
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Code == "" {
		response.Code = "unexpected_response"
	}
	apiErr := &apiError{StatusCode: statusCode, Code: response.Code}
	switch statusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %w", ErrNotFound, apiErr)
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTooManyRequests:
		return fmt.Errorf("%w: %w", ErrUnavailable, apiErr)
	default:
		return apiErr
	}
}

func validateExtractRequest(request ExtractRequest) error {
	if (request.UploadID == "") == (request.JobID == "") {
		return fmt.Errorf("%w: exactly one upload_id or job_id is required", ErrInvalidRequest)
	}
	if request.UploadID != "" && !uploadIDPattern.MatchString(request.UploadID) {
		return fmt.Errorf("%w: upload_id is invalid", ErrInvalidRequest)
	}
	if request.JobID != "" && !jobIDPattern.MatchString(request.JobID) {
		return fmt.Errorf("%w: job_id is invalid", ErrInvalidRequest)
	}
	if _, ok := documentTypes[request.DocumentType]; !ok {
		return fmt.Errorf("%w: document_type is invalid", ErrInvalidRequest)
	}
	if _, ok := outputFormats[request.OutputFormat]; !ok {
		return fmt.Errorf("%w: output_format is invalid", ErrInvalidRequest)
	}
	if len(request.LanguageHints) > 8 {
		return fmt.Errorf("%w: too many language hints", ErrInvalidRequest)
	}
	for _, hint := range request.LanguageHints {
		if len(hint) < 2 || len(hint) > 35 {
			return fmt.Errorf("%w: language hint is invalid", ErrInvalidRequest)
		}
	}
	if request.Schema != nil && (request.Schema.SchemaID == "" || request.Schema.SchemaVersion == "") {
		return fmt.Errorf("%w: schema reference is incomplete", ErrInvalidRequest)
	}
	return nil
}

func isLocalHTTPHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".svc.cluster.local") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func outputOptionsFor(format string, evidence bool) outputOptions {
	return outputOptions{
		Text:     format == "text",
		Markdown: format == "markdown",
		Layout:   format == "structured",
		Evidence: evidence,
	}
}

func resultAvailable(status string) bool {
	return status == "completed" || status == "partial" || status == "review_required"
}

func pendingResponse(jobID string, status string) ExtractResponse {
	return ExtractResponse{
		JobID:              jobID,
		Status:             status,
		ContentTrust:       "untrusted",
		Warnings:           []string{},
		ValidationFailures: []ValidationFailure{},
	}
}

func mapResult(status jobResponse, result documentResult) (ExtractResponse, error) {
	if err := validateDocumentResult(result); err != nil {
		return ExtractResponse{}, err
	}
	names := make([]string, 0, len(result.Fields))
	for name := range result.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make([]Field, 0, len(names))
	for _, name := range names {
		value := result.Fields[name]
		encoded, err := json.Marshal(value.Value)
		if err != nil {
			return ExtractResponse{}, fmt.Errorf("encode extracted field %s: %w", name, err)
		}
		fields = append(fields, Field{
			Name:       name,
			ValueJSON:  string(encoded),
			Confidence: value.Confidence,
			Citations:  citationsFor(result.DocumentVersion, value.Evidence),
		})
	}
	tables := make([]Table, 0, len(result.Tables))
	for _, table := range result.Tables {
		cells := make([]TableCell, 0, len(table.Cells))
		for _, cell := range table.Cells {
			cells = append(cells, TableCell{
				Row:        cell.Row,
				Column:     cell.Column,
				Text:       cell.Text,
				Confidence: cell.Confidence,
				Citations:  citationsFor(result.DocumentVersion, cell.Evidence),
			})
		}
		tables = append(tables, Table{TableID: table.TableID, Cells: cells})
	}
	pages := make([]DocumentPage, 0, len(result.Pages))
	for _, page := range result.Pages {
		observations := make([]TextObservation, 0, len(page.Observations))
		for _, observation := range page.Observations {
			points := make([][]float64, 0, len(observation.Polygon.Points))
			for _, point := range observation.Polygon.Points {
				points = append(points, []float64{point.X, point.Y})
			}
			observations = append(observations, TextObservation{
				ObservationID:       observation.ObservationID,
				Level:               observation.Level,
				Text:                observation.Text,
				Confidence:          observation.Confidence,
				Polygon:             points,
				ReadingOrder:        observation.ReadingOrder,
				ParentObservationID: observation.ParentObservationID,
			})
		}
		pages = append(pages, DocumentPage{
			Page: page.Page, Width: page.Width, Height: page.Height, Observations: observations,
		})
	}
	return ExtractResponse{
		JobID:               status.JobID,
		Status:              status.Status,
		ContentTrust:        result.ContentTrust,
		ResultSchemaVersion: result.SchemaVersion,
		DocumentID:          result.DocumentID,
		DocumentVersion:     result.DocumentVersion,
		Text:                result.Text,
		Markdown:            result.Markdown,
		Pages:               pages,
		Fields:              fields,
		Tables:              tables,
		Confidence:          result.Confidence,
		Citations:           citationsFor(result.DocumentVersion, result.Citations),
		Warnings:            append([]string{}, result.Warnings...),
		ValidationFailures:  append([]ValidationFailure{}, result.ValidationFailures...),
		Provider:            result.Provider,
		ModelVersion:        result.ModelVersion,
		ProcessingProfile:   result.ProcessingProfile,
		DurationMS:          result.DurationMS,
		Cost:                result.Cost,
	}, nil
}

func citationsFor(documentVersion string, evidenceItems []evidence) []Citation {
	citations := make([]Citation, 0, len(evidenceItems))
	for _, item := range evidenceItems {
		points := make([][]float64, 0, len(item.Polygon.Points))
		for _, point := range item.Polygon.Points {
			points = append(points, []float64{point.X, point.Y})
		}
		citations = append(citations, Citation{
			DocumentVersion: documentVersion,
			Page:            item.Page,
			Polygon:         points,
			ObservationID:   item.ObservationID,
		})
	}
	return citations
}

func validateJobResponse(response jobResponse) error {
	if !jobIDPattern.MatchString(response.JobID) {
		return fmt.Errorf("document intelligence returned an invalid job_id")
	}
	if _, ok := jobStatuses[response.Status]; !ok {
		return fmt.Errorf("document intelligence returned an invalid job status")
	}
	if response.CreatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, response.CreatedAt); err != nil {
			return fmt.Errorf("document intelligence returned an invalid creation time")
		}
	}
	expectedStatusURL := "/v1/ocr/jobs/" + url.PathEscape(response.JobID)
	if response.StatusURL != "" && response.StatusURL != expectedStatusURL {
		return fmt.Errorf("document intelligence returned an invalid status locator")
	}
	if response.ResultURL != "" && response.ResultURL != expectedStatusURL+"/result" {
		return fmt.Errorf("document intelligence returned an invalid result locator")
	}
	return nil
}

func validateDocumentResult(result documentResult) error {
	if result.SchemaVersion != "1.0" {
		return fmt.Errorf("document intelligence returned an unsupported result schema")
	}
	if !documentIDPattern.MatchString(result.DocumentID) || !digestPattern.MatchString(result.DocumentVersion) {
		return fmt.Errorf("document intelligence returned invalid document identity")
	}
	if result.ContentTrust != "untrusted" {
		return fmt.Errorf("document intelligence returned invalid content trust")
	}
	if !validPages(result.Pages) {
		return fmt.Errorf("document intelligence returned invalid pages")
	}
	for name, field := range result.Fields {
		if name == "" || len(name) > 128 || math.IsNaN(field.Confidence) || field.Confidence < 0 || field.Confidence > 1 {
			return fmt.Errorf("document intelligence returned an invalid extracted field")
		}
		if len(field.Evidence) == 0 {
			return fmt.Errorf("document intelligence returned a field without evidence")
		}
		if !validEvidence(field.Evidence) {
			return fmt.Errorf("document intelligence returned invalid field evidence")
		}
	}
	if (result.Text != "" || result.Markdown != "") && len(result.Citations) == 0 {
		return fmt.Errorf("document intelligence returned content without evidence")
	}
	if !validEvidence(result.Citations) {
		return fmt.Errorf("document intelligence returned invalid document evidence")
	}
	for _, table := range result.Tables {
		if !tableIDPattern.MatchString(table.TableID) || len(table.Cells) == 0 || len(table.Cells) > 10000 {
			return fmt.Errorf("document intelligence returned an invalid table")
		}
		for _, cell := range table.Cells {
			if !validConfidence(cell.Confidence) || len(cell.Evidence) == 0 || !validEvidence(cell.Evidence) {
				return fmt.Errorf("document intelligence returned an invalid table cell")
			}
		}
	}
	if result.Confidence != nil && !validConfidenceDimensions(*result.Confidence) {
		return fmt.Errorf("document intelligence returned invalid confidence")
	}
	for _, warning := range result.Warnings {
		if !codePattern.MatchString(warning) {
			return fmt.Errorf("document intelligence returned an invalid warning")
		}
	}
	for _, failure := range result.ValidationFailures {
		if !codePattern.MatchString(failure.Code) || (failure.Severity != "warning" && failure.Severity != "error") {
			return fmt.Errorf("document intelligence returned an invalid validation failure")
		}
	}
	for _, version := range []string{result.Provider, result.ModelVersion, result.ProcessingProfile} {
		if version != "" && !versionPattern.MatchString(version) {
			return fmt.Errorf("document intelligence returned invalid processing provenance")
		}
	}
	if result.Cost != nil && (!currencyPattern.MatchString(result.Cost.Currency) || !decimalPattern.MatchString(result.Cost.Decimal)) {
		return fmt.Errorf("document intelligence returned invalid cost")
	}
	return nil
}

func validEvidence(items []evidence) bool {
	for _, item := range items {
		if item.Page == 0 || !observationPattern.MatchString(item.ObservationID) || !validPolygon(item.Polygon.Points) {
			return false
		}
	}
	return true
}

func validPages(pages []documentPage) bool {
	if len(pages) > 300 {
		return false
	}
	seenPages := make(map[uint32]struct{}, len(pages))
	for _, page := range pages {
		if page.Page == 0 || page.Width == 0 || page.Height == 0 || len(page.Observations) > 100000 {
			return false
		}
		if _, exists := seenPages[page.Page]; exists {
			return false
		}
		seenPages[page.Page] = struct{}{}
		seenIDs := make(map[string]struct{}, len(page.Observations))
		seenOrders := make(map[uint32]struct{}, len(page.Observations))
		for _, observation := range page.Observations {
			if !observationPattern.MatchString(observation.ObservationID) ||
				(observation.Level != "page" && observation.Level != "paragraph" && observation.Level != "line" && observation.Level != "word") ||
				strings.TrimSpace(observation.Text) == "" || len(observation.Text) > 65536 ||
				!validConfidence(observation.Confidence) || !validPolygon(observation.Polygon.Points) {
				return false
			}
			if _, exists := seenIDs[observation.ObservationID]; exists {
				return false
			}
			if _, exists := seenOrders[observation.ReadingOrder]; exists {
				return false
			}
			if observation.ParentObservationID != nil {
				if _, exists := seenIDs[*observation.ParentObservationID]; !exists {
					return false
				}
			}
			seenIDs[observation.ObservationID] = struct{}{}
			seenOrders[observation.ReadingOrder] = struct{}{}
		}
	}
	return true
}

func validConfidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validConfidenceDimensions(value Confidence) bool {
	return validConfidence(value.InputQuality) && validConfidence(value.OCR) &&
		validConfidence(value.Classification) && validConfidence(value.Extraction) &&
		validConfidence(value.Validation) && validConfidence(value.Overall)
}

func validPolygon(points []point) bool {
	if len(points) < 3 {
		return false
	}
	doubleArea := 0.0
	for index, left := range points {
		right := points[(index+1)%len(points)]
		if math.IsNaN(left.X) || math.IsNaN(left.Y) || left.X < 0 || left.X > 1 || left.Y < 0 || left.Y > 1 {
			return false
		}
		doubleArea += left.X*right.Y - right.X*left.Y
	}
	return math.Abs(doubleArea) > 1e-12
}

var documentTypes = map[string]struct{}{
	"auto": {}, "general": {}, "invoice": {}, "receipt": {}, "purchase_order": {},
	"identity_document": {}, "contract": {}, "bank_statement": {}, "medical_form": {},
	"application_form": {}, "resume": {},
}

var outputFormats = map[string]struct{}{"structured": {}, "text": {}, "markdown": {}}

var jobStatuses = map[string]struct{}{
	"accepted": {}, "inspecting": {}, "processing": {}, "validating": {}, "cancelling": {},
	"cancelled": {}, "rejected": {}, "partial": {}, "review_required": {}, "completed": {},
}

type source struct {
	UploadID string `json:"upload_id"`
}

type outputOptions struct {
	Text     bool `json:"text"`
	Markdown bool `json:"markdown"`
	Layout   bool `json:"layout"`
	Evidence bool `json:"evidence"`
}

type createJobRequest struct {
	Source        source           `json:"source"`
	DocumentType  string           `json:"document_type"`
	Output        outputOptions    `json:"output"`
	Extraction    *SchemaReference `json:"extraction,omitempty"`
	LanguageHints []string         `json:"language_hints,omitempty"`
}

type jobResponse struct {
	JobID     string `json:"job_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	StatusURL string `json:"status_url"`
	ResultURL string `json:"result_url"`
}

type documentResult struct {
	SchemaVersion      string                    `json:"schema_version"`
	DocumentID         string                    `json:"document_id"`
	DocumentVersion    string                    `json:"document_version"`
	ContentTrust       string                    `json:"content_trust"`
	Text               string                    `json:"text"`
	Markdown           string                    `json:"markdown"`
	Pages              []documentPage            `json:"pages"`
	Fields             map[string]extractedValue `json:"fields"`
	Tables             []documentTable           `json:"tables"`
	Confidence         *Confidence               `json:"confidence"`
	Citations          []evidence                `json:"citations"`
	Warnings           []string                  `json:"warnings"`
	ValidationFailures []ValidationFailure       `json:"validation_failures"`
	Provider           string                    `json:"provider"`
	ModelVersion       string                    `json:"model_version"`
	ProcessingProfile  string                    `json:"processing_profile_version"`
	DurationMS         uint64                    `json:"duration_ms"`
	Cost               *Cost                     `json:"cost"`
}

type documentPage struct {
	Page         uint32            `json:"page"`
	Width        uint32            `json:"width"`
	Height       uint32            `json:"height"`
	Observations []textObservation `json:"observations"`
}

type textObservation struct {
	ObservationID       string  `json:"observation_id"`
	Level               string  `json:"level"`
	Text                string  `json:"text"`
	Confidence          float64 `json:"confidence"`
	Polygon             polygon `json:"polygon"`
	ReadingOrder        uint32  `json:"reading_order"`
	ParentObservationID *string `json:"parent_observation_id"`
}

type extractedValue struct {
	Value      any        `json:"value"`
	Confidence float64    `json:"confidence"`
	Evidence   []evidence `json:"evidence"`
}

type documentTable struct {
	TableID string              `json:"table_id"`
	Cells   []documentTableCell `json:"cells"`
}

type documentTableCell struct {
	Row        uint32     `json:"row"`
	Column     uint32     `json:"column"`
	Text       string     `json:"text"`
	Confidence float64    `json:"confidence"`
	Evidence   []evidence `json:"evidence"`
}

type evidence struct {
	Page          uint32  `json:"page"`
	ObservationID string  `json:"observation_id"`
	Polygon       polygon `json:"polygon"`
}

type polygon struct {
	Points []point `json:"points"`
}

type point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
