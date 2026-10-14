# X-IAM Historical Reference Index

Status: index of privately retained planning references
Created: 2026-07-08
Public-candidate disposition: the 15 original reference copies are excluded; their exact bytes and digests are retained privately.

## Purpose

This index records which X-IAM materials informed earlier X-Insight planning. The original files are not distributed with this public source candidate. Their historical filenames below identify private sources, not accessible repository paths. This index is not a claim of permission to redistribute the originals.

These files are references, not authoritative PRDs. Several X-IAM documents were generated during earlier AI-consultant discussions and must be treated as source material requiring review, not as settled product decisions.

## Privately Retained Source Sets

### `source-copies/2026-06-01`

Selected because these files directly relate to X-Insight product planning:

- `x-insight-product-roadmap.md`
- `x-iam-project-full-review.md`
- `x-insight-naming-and-pillars-analysis.md`
- `phase-0-maas-differentiation.md`
- `phase-1-trace-mvp.md`
- `phase-2-cost-attribution.md`
- `phase-3-authorization-detection.md`
- `phase-4-behavior-analysis.md`
- `phase-5-ontology-graph.md`
- `phase0-5-discussion-questions.md`
- `x-iam-discussion_Round4-summary.md`

Primary value:

- phase sequencing;
- Trace MVP assumptions;
- cost attribution ideas;
- authorization and MCP threat framing;
- behavior/process mining direction;
- ontology/graph caution and future expansion ideas.

Main caveat:

- These documents are more ambitious than the current X-Insight convergence direction and should not override the evidence-contract-first strategy.

### `source-copies/research`

Selected because these files help preserve source reliability and research provenance:

- `advisor-paper-source-verification.md`
- `x-iam-research-papers-v2.md`
- `round2-source-index.md`
- `round2-supplemental-research.md`

Primary value:

- identifies verified versus hallucinated research claims;
- prevents low-quality advisor outputs from becoming product assumptions;
- provides a reusable reference list for identity, MCP, process mining, ontology, and governance research.

Main caveat:

- Research links and paper status may drift; verify again before using a claim in PRD, marketing, or architecture decisions.

## How To Read This Index

The original materials served as background references for:

- adapter roadmap;
- evidence contract expansion;
- SDK/MCP instrumentation;
- identity and delegation model;
- capture-gap taxonomy;
- later behavior analytics and ontology discussions.

Public conclusions that depend on these private references need independently accessible evidence. Do not treat this index as an implementation specification or verification of their claims; reconcile any proposal against:

- current X-Insight discussion docs;
- current UniRouter code;
- current one-api/new-api/LiteLLM source;
- customer deployment constraints;
- privacy and retention decisions.
