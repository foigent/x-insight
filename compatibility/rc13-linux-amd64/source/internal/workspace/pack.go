package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"time"
)

const EvidencePackSchema = "xinsight.evidence-pack.v1"

type EvidencePack struct {
	SchemaVersion     string          `json:"schema_version"`
	CaseID            string          `json:"case_id"`
	Revision          int64           `json:"revision"`
	SealedAt          time.Time       `json:"sealed_at"`
	SealedBySubject   string          `json:"sealed_by_subject"`
	Title             string          `json:"title"`
	Description       string          `json:"description"`
	RedactionPolicy   string          `json:"redaction_policy"`
	ContentBoundary   string          `json:"content_boundary"`
	UserContentPolicy string          `json:"user_content_policy"`
	Executions        []CaseExecution `json:"executions"`
	Findings          []Finding       `json:"findings"`
	Evidence          []EvidenceLink  `json:"evidence"`
	Gaps              []GapLink       `json:"gaps"`
	Alerts            []AlertCaseLink `json:"alerts"`
	Annotations       []Annotation    `json:"annotations"`
}

func BuildEvidencePack(record CaseRecord, sealedAt time.Time, subject string, revision int64) (EvidencePack, []byte, string, error) {
	if revision <= 0 || sealedAt.IsZero() || !subjectPattern.MatchString(subject) {
		return EvidencePack{}, nil, "", ErrInvalid
	}
	pack := EvidencePack{
		SchemaVersion:     EvidencePackSchema,
		CaseID:            record.Case.ID,
		Revision:          revision,
		SealedAt:          sealedAt.UTC(),
		SealedBySubject:   subject,
		Title:             record.Case.Title,
		Description:       record.Case.Description,
		RedactionPolicy:   "metadata_allowlist_no_raw_v1",
		ContentBoundary:   "product-local investigation records and Alert links plus immutable M0 identity, hash, source-ref and Gap snapshots; Alert state is snapshotted at seal time; no source raw payload or mutable delivery state",
		UserContentPolicy: "user-authored title, description, findings and annotations are included verbatim; restricted raw source content must not be pasted into the Case",
		Executions:        append([]CaseExecution(nil), record.Executions...),
		Findings:          append([]Finding(nil), record.Findings...),
		Evidence:          append([]EvidenceLink(nil), record.Evidence...),
		Gaps:              append([]GapLink(nil), record.Gaps...),
		Alerts:            append([]AlertCaseLink(nil), record.Alerts...),
		Annotations:       append([]Annotation(nil), record.Annotations...),
	}
	sortPack(&pack)
	payload, err := json.Marshal(pack)
	if err != nil {
		return EvidencePack{}, nil, "", fmt.Errorf("encode Evidence Pack: %w", err)
	}
	digest := sha256.Sum256(payload)
	return pack, payload, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func sortPack(pack *EvidencePack) {
	sort.Slice(pack.Executions, func(left, right int) bool {
		if pack.Executions[left].ExecutionID == pack.Executions[right].ExecutionID {
			return pack.Executions[left].AsOf.Before(pack.Executions[right].AsOf)
		}
		return pack.Executions[left].ExecutionID < pack.Executions[right].ExecutionID
	})
	sort.Slice(pack.Findings, func(left, right int) bool {
		return pack.Findings[left].ID < pack.Findings[right].ID
	})
	sort.Slice(pack.Evidence, func(left, right int) bool {
		if pack.Evidence[left].EvidenceEventID == pack.Evidence[right].EvidenceEventID {
			if pack.Evidence[left].Relation == pack.Evidence[right].Relation {
				return pack.Evidence[left].FindingID < pack.Evidence[right].FindingID
			}
			return pack.Evidence[left].Relation < pack.Evidence[right].Relation
		}
		return pack.Evidence[left].EvidenceEventID < pack.Evidence[right].EvidenceEventID
	})
	sort.Slice(pack.Gaps, func(left, right int) bool {
		if pack.Gaps[left].GapID == pack.Gaps[right].GapID {
			if pack.Gaps[left].Relation == pack.Gaps[right].Relation {
				return pack.Gaps[left].FindingID < pack.Gaps[right].FindingID
			}
			return pack.Gaps[left].Relation < pack.Gaps[right].Relation
		}
		return pack.Gaps[left].GapID < pack.Gaps[right].GapID
	})
	sort.Slice(pack.Alerts, func(left, right int) bool {
		return pack.Alerts[left].AlertID < pack.Alerts[right].AlertID
	})
	sort.Slice(pack.Annotations, func(left, right int) bool {
		return pack.Annotations[left].ID < pack.Annotations[right].ID
	})
}

func RenderEvidencePackHTML(pack EvidencePack, digest string) ([]byte, error) {
	if !hashPattern.MatchString(digest) {
		return nil, ErrInvalid
	}
	var output bytes.Buffer
	if err := evidencePackTemplate.Execute(&output, struct {
		Pack   EvidencePack
		Digest string
	}{Pack: pack, Digest: digest}); err != nil {
		return nil, fmt.Errorf("render Evidence Pack: %w", err)
	}
	return output.Bytes(), nil
}

var evidencePackTemplate = template.Must(template.New("evidence-pack").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Pack.Title}} · X-Insight Evidence Pack</title>
<style>
:root{color-scheme:light;font-family:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#16202a;background:#fff}
body{max-width:960px;margin:0 auto;padding:40px 32px;line-height:1.55}
h1,h2{line-height:1.2}h2{margin-top:32px;border-bottom:1px solid #d9dee3;padding-bottom:8px}
code{font-family:"SFMono-Regular",Consolas,monospace;font-size:.88em;overflow-wrap:anywhere}
.meta{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px 24px;padding:16px;background:#f1f3f5;border:1px solid #d9dee3;border-radius:8px}
.item{break-inside:avoid;border:1px solid #d9dee3;border-radius:8px;padding:12px 16px;margin:10px 0}
.gap{border-style:dashed;border-color:#a96512;background:#fff6e7}
@media(max-width:600px){body{padding:24px 16px}.meta{grid-template-columns:1fr}}
@media print{body{max-width:none;padding:0}.no-print{display:none}.item{box-shadow:none}section{break-before:auto}h2{break-after:avoid}a{color:inherit;text-decoration:none}a[href]::after{content:" (" attr(href) ")";font-size:.8em}}
</style>
</head>
<body>
<header>
<p>X-Insight · sealed evidence dossier</p>
<h1>{{.Pack.Title}}</h1>
<p>{{.Pack.Description}}</p>
<div class="meta">
<span>Case <code>{{.Pack.CaseID}}</code></span>
<span>Revision {{.Pack.Revision}}</span>
<span>Sealed <time>{{.Pack.SealedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></span>
<span>Digest <code>{{.Digest}}</code></span>
<span>Actor <code>{{.Pack.SealedBySubject}}</code></span>
<span>Policy <code>{{.Pack.RedactionPolicy}}</code></span>
</div>
</header>
<main>
<section><h2>Scope and redaction</h2><p><code>{{.Pack.RedactionPolicy}}</code></p><p>{{.Pack.ContentBoundary}}</p><p><strong>User content:</strong> {{.Pack.UserContentPolicy}}</p><p>This dossier describes one sealed Case revision. Absence of a linked Gap does not establish global source completeness.</p></section>
<section><h2>Findings</h2>{{range .Pack.Findings}}<article class="item"><h3>{{.Title}}</h3><p>{{.Statement}}</p><p><code>{{.Classification}}</code> · <code>{{.Status}}</code></p></article>{{else}}<p>No findings recorded.</p>{{end}}{{range .Pack.Annotations}}<article class="item"><p>{{.Body}}</p><code>annotation · {{.TargetKind}} · {{.TargetRef}}</code></article>{{end}}</section>
<section><h2>Execution stories and Alerts</h2>{{range .Pack.Executions}}<article class="item"><code>{{.ExecutionID}}</code><br><time>{{.AsOf.Format "2006-01-02T15:04:05.000000Z07:00"}}</time><p>Story remains addressable at this fixed Evidence snapshot.</p></article>{{else}}<p>No executions linked.</p>{{end}}{{range .Pack.Alerts}}<article class="item"><code>{{.AlertID}}</code><p>Source state at seal: <code>{{.AlertState}}</code></p></article>{{else}}<p>No source Alerts linked.</p>{{end}}</section>
	<section><h2>Completeness and Capture Gaps</h2>{{range .Pack.Gaps}}<article class="item gap"><strong>{{.GapTypeSnapshot}}</strong><p>{{.AffectedClaimSnapshot}}</p><code>{{.GapID}}</code><p>{{.Relation}}</p>{{if .FindingID}}<p>Finding <code>{{.FindingID}}</code></p>{{else}}<p>Case-level limitation</p>{{end}}</article>{{else}}<p>No Capture Gaps are linked to this Case. This does not prove global completeness.</p>{{end}}</section>
	<section><h2>Evidence citations</h2>{{range .Pack.Evidence}}<article class="item"><code>{{.EvidenceEventID}}</code><br><code>{{.EventHashSnapshot}}</code><br><code>{{.SourceRefSnapshot}}</code><p>{{.Relation}}</p>{{if .FindingID}}<p>Finding <code>{{.FindingID}}</code></p>{{else}}<p>Case-level citation</p>{{end}}</article>{{else}}<p>No Evidence linked.</p>{{end}}</section>
<section><h2>Provenance appendix</h2>{{range .Pack.Executions}}<article class="item"><code>{{.ExecutionID}}</code><p>linked by <code>{{.AddedBySubject}}</code> at <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Findings}}<article class="item"><code>{{.ID}}</code><p>created by <code>{{.CreatedBySubject}}</code> at <time>{{.CreatedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Evidence}}<article class="item"><code>{{.EvidenceEventID}}</code><p>linked by <code>{{.AddedBySubject}}</code> at <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Gaps}}<article class="item"><code>{{.GapID}}</code><p>linked by <code>{{.AddedBySubject}}</code> at <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Alerts}}<article class="item"><code>{{.AlertID}}</code><p>linked by <code>{{.LinkedBySubject}}</code> at <time>{{.LinkedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}</section>
<section><h2>Verification digest</h2><p><code>{{.Digest}}</code></p><p>Algorithm: SHA-256 over the exact compact JSON download bytes for this sealed revision. The HTML presentation is not part of that digest.</p></section>
</main>
</body>
</html>`))
