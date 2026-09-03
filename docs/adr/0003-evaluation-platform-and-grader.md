# ADR-0003: Evaluation platform — shared eval store, grader service, Langfuse

- Status: Proposed
- Date: 2026-09-03
- Owner: Samyak Rout
- Relates to: [PRD](../PRD.md) §6.4, §19; [ADR-0002](0002-shared-brain-and-learning-flywheel.md) D3, D5, D7; [PLAN](../PLAN.md) Phase 3.1–3.4
- Supersedes: none

## Context

ADR-0002 makes the eval harness the promotion gate for every learned artefact,
and PLAN Phase 3 asks for a golden set and a harness before anything else in
Track B can start. Nobody owns either yet (brain LLD §16.2). Meanwhile the
family already has three unconnected ways of evaluating an agent:

| Where | What exists | Where results go |
| --- | --- | --- |
| `tesserix-adk` `evals` package | JSONL golden datasets, cassette replay, `Rubric` + `LlmJudge` with kappa calibration, metrics protocol, `Baseline` regression gate | caller's choice |
| DevAI | `eval_datasets`, `eval_suites` with a `scorers[]` array, `eval_runs`, `eval_case_results`, `eval_comparisons` | `devai_db` on devai-postgres; scores mirrored fail-open to Langfuse |
| `ai-agents` (Kora, SRE) | hand-rolled YAML suites, assertion-style expectations, live gateway calls | stdout only |

Two eval databases exist: DevAI's run store, and `devai_evals_db` on the global
CNPG cluster holding operator-synced anonymised product snapshots. Australis,
the OCR service and every future product would each add a fourth and fifth
unless one store and one grader are named now.

The observability estate is split the same way. Langfuse (v4, ClickHouse
backed, Zitadel SSO) is live; the custom pipeline (OTel → Redpanda → ClickHouse
→ obs-api/obs-ui) is parked at zero replicas since 2026-08-01.

### Numbers

| Quantity | Assumption | Derivation |
| --- | --- | --- |
| Products graded | 5 (Kora, home-chef, mark8ly, OCR, SRE agent) | current fleet + PRD roadmap |
| Cases per golden set | 50–500 | DevAI cap is 50; Kora coaching needs more |
| Runs per day | ≤ 200 | PR gates + nightly per suite + ad-hoc |
| Rows per run | cases × criteria ≈ 500 × 8 = 4,000 | one score row per criterion |
| Score rows per month | ≈ 24 M worst case | 200 × 4,000 × 30 |
| Store growth | < 10 GB / year | ~300 B per score row, indexed |
| Judge cost per run | < $2 | 500 cases × 1 judge call × frontier-mini pricing |

Nothing here needs more than a single Postgres with a correctly sized WAL.

## Options considered

1. **Keep DevAI's tables as the store, point everything at devai-postgres.**
   Couples every product to a DevAI deployment and inherits the 50-case cap.
2. **Langfuse datasets and scores as the store of record.** Langfuse holds
   scores in ClickHouse, which is analytical and eventually consistent; a gate
   that reads its own write moments later cannot rely on it, and rubric
   calibration has no home there.
3. **A shared `evals_db` on global CNPG, one grader service, Langfuse as the
   trace sink.** Chosen.

## Decision

### D1 — Global CNPG is the eval store of record

`evals_db` on `global-postgres` holds golden datasets, cases, versioned
rubrics, judge calibrations, suites, runs, per-criterion scores, human labels,
baselines and comparisons, all under one `eval` schema. Schema lives in
`tesserix-k8s` (`schemas/global/global/evals_db.sql`), written by role
`grader`, per-table grants only. Anonymised product snapshots stay in
`devai_evals_db`, fed by the sandbox-data-sync operator; the grader reads them,
never writes them.

Every case carries a `tenant`; a run whose answer came back under another
tenant is an error, not a score. This is ADR-0002 invariant I1 applied to
evaluation.

### D2 — One grader service, criteria as pluggable scorers

A Python service on the ADK base image accepts
`(suite, target, criteria[])` and produces one run. Targets are reached over
A2A or an OpenAI-compatible URL, so ADK agents, the Australis engine and the OCR
service enter the same door. Scoring is the ADK `evals` package; the service
adds durability, scheduling, a stable API and the Langfuse export. A new
criterion is a scorer registration, never a service change.

Three grader kinds, each a first-class row in `eval.scores` with
`grader_kind` and `grader_ref`, so one case can carry all three side by side:

| Kind | What it is | Examples | Gate role |
| --- | --- | --- | --- |
| **code** | deterministic function over case, output and evidence | exact match, schema validity, groundedness, citation coverage, refusal correctness, tool sequence, cost, latency, tokens; OCR CER/WER, page and region accuracy, layout kind | always gates; cheapest, runs on every case |
| **model** | calibrated LLM judge scoring a versioned rubric | helpfulness, clinical caution, tone, faithfulness beyond citation markers, pairwise preference | gates only above its calibration floor (D3) |
| **human** | reviewer verdict on a rubric, through `eval.review_requests` | expert review, spot-check of flagged or disagreeing cases, calibration labels | strongest signal, scarcest; seeds judge calibration and is the D3 expert-review signal in ADR-0002 |

Human review is not an afterthought: every model-judged case the judge
flagged, every case where two graders disagree, and a fixed random sample per
run enter the review queue. Reviews land as `human` scores and, for rubric
cases, as calibration labels, so the judge's floor is re-measured from real
traffic rather than a one-off labelled set.

### D3 — A judge earns the right to gate

Every LLM-judged score records `rubric@version/model/prompt_version`. A judge
gates only when its stored calibration against human labels clears the rubric's
floor (default kappa 0.6). No calibration, one-score-fits-all, or drift from a
rubric edit fails closed, exactly as the ADK's `Calibration.require` does.

### D4 — Unknown is a reason, never a zero

A score row carries either a value or an `unavailable_reason`. Unpriced
models, missing usage blocks and cancelled runs are counted as unknown and kept
out of aggregates. A case that errored or did not finish is never a pass.

### D5 — Langfuse and ClickHouse hold AI traces only

Every run and case result is exported fail-open to Langfuse with the ADK
`adk.*` attribute convention, so scores, traces and per-tenant cost sit on one
screen. Langfuse and the ClickHouse cluster behind it hold **AI traces, spans,
generations and scores and nothing else**: no application logs, no Kubernetes
events, no node or pod metrics. Those stay in Cloud Logging and Cloud
Monitoring, where GKE already puts them.

Transport (decided 2026-09-03): agents export OTLP to `otel-gateway`, which
drops every span not marked `tesserix.signal=ai`, spools to the Redpanda topic
`ai.traces` (RF 3, 7 day retention), and `otel-ingest` routes each product's
spans by `service.namespace` to that product's Langfuse project with its own
key pair. The buffer is what makes the sink optional: a Langfuse outage or a
missing project key accumulates in Redpanda and is replayed by seeking the
consumer group, and derived ids (D6) make the replay an upsert. The `filelog`,
`k8sobjects` and `prometheus` receivers, the ClickHouse exporter and the
`otel.*` log, metric and event topics are not revived. Neither sink can fail a
run: Postgres is the only write that blocks.

Langfuse v4 runs in events-only mode: `/api/public/traces` answers 404 and
readers use the observations and metrics APIs instead. Anything that needs to
read a trace back, the grader included, must be written against those.

### D6 — Every AI trace carries deterministic ids

A trace that cannot be joined to a run cannot be graded, and a run that cannot
be replayed under the same id cannot be compared. Every AI request, in
production or under evaluation, carries:

| Field | Source | Rule |
| --- | --- | --- |
| `tenant` | bound tenant scope | never defaulted; a span without one is refused |
| `session_id` | product conversation or job id | stable for the life of the conversation |
| `run_id` | ADK runner | one per agent invocation; the Langfuse trace id |
| `eval_run_id`, `case_id` | grader | present only under evaluation; derived from suite, dataset version and case seed, so a rerun on another machine produces the same ids |
| `config_revision`, `agent_version`, `model` | deployment | pins what answered |

Ids are derived, never random, under evaluation: the same suite version and
case always yield the same `run_id`, so a rerun upserts rather than duplicates,
and Langfuse scores attach to exactly one trace. Production `run_id` values are
the ADK's ULIDs and are idempotency keys for capture (brain LLD §9).

### D7 — Promotion reuses the ADK baseline gate

The gate compares a candidate run against the suite's baseline per metric per
case, with a noise band and quarantine, and writes a `comparison` row. Promotion
is a human action that moves the baseline pointer; the previous baseline stays
for rollback (ADR-0002 D5).

## Consequences

### Positive

- PLAN 3.1 and 3.2 are satisfied by a service that already exists in library
  form; Australis's `australis-evals` MCP server becomes a thin wrapper.
- One store lets T0 policy fitting (Phase 4) query `(config → metric)` pairs
  across products without a second warehouse.
- Kora's YAML suites port to JSONL datasets and gain replay, calibration and a
  regression gate for free.

### Negative — accepted

- Global CNPG is a single instance today; the WAL volume is raised to 16 GiB
  and replicas must be re-enabled before the grader takes PR traffic.
- The sandbox sync claim for Kora covers one table; golden sets need food logs
  and reference foods synced before retrieval criteria mean anything.
- Calibration needs human labels. Golden-set authorship (brain LLD §16.2) is
  still unowned and now blocks judge-based criteria, not just datasets.

### Cost

Storage under $2/month at the numbers above; judge spend bounded per run and
recorded on every score row. Reviving the custom pipeline is a separate cost
decision: a three-node pool plus roughly 390 GiB of storage, taken only when a
failure needs it.

## Open questions

1. Who authors and labels the first Kora golden set, and at what cadence.
2. Whether the grader runs cases in-cluster against staging or replays
   cassettes for PR gates; the ADK supports both, cost favours replay.
3. OCR ground truth format and where document blobs live (GCS key on the case
   is assumed).
