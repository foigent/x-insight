package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const EvidencePackSchemaV1 = "xinsight.evidence-pack.v1"
const EvidencePackSchemaV2 = "xinsight.evidence-pack.v2"
const EvidencePackSchema = "xinsight.evidence-pack.v3"
const ResourceEvidencePackSchema = "xinsight.evidence-pack.v4"

type EvidencePack struct {
	Contents          []ContentLink       `json:"contents,omitempty"`
	Resources         []ResourceLink      `json:"resources,omitempty"`
	Assessments       *AssessmentSnapshot `json:"assessments,omitempty"`
	Responsibility    *FollowupSnapshot   `json:"responsibility,omitempty"`
	SchemaVersion     string              `json:"schema_version"`
	CaseID            string              `json:"case_id"`
	Revision          int64               `json:"revision"`
	SealedAt          time.Time           `json:"sealed_at"`
	SealedBySubject   string              `json:"sealed_by_subject"`
	Title             string              `json:"title"`
	Description       string              `json:"description"`
	RedactionPolicy   string              `json:"redaction_policy"`
	ContentBoundary   string              `json:"content_boundary"`
	UserContentPolicy string              `json:"user_content_policy"`
	Executions        []CaseExecution     `json:"executions"`
	Findings          []Finding           `json:"findings"`
	Evidence          []EvidenceLink      `json:"evidence"`
	Gaps              []GapLink           `json:"gaps"`
	Alerts            []AlertCaseLink     `json:"alerts"`
	Annotations       []Annotation        `json:"annotations"`
}

func BuildEvidencePack(record CaseRecord, sealedAt time.Time, subject string, revision int64) (EvidencePack, []byte, string, error) {
	if revision <= 0 || sealedAt.IsZero() || !subjectPattern.MatchString(subject) {
		return EvidencePack{}, nil, "", ErrInvalid
	}
	pack := EvidencePack{
		Contents:          append([]ContentLink(nil), record.Contents...),
		Assessments:       record.Assessments,
		Responsibility:    record.Responsibility,
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
		Resources:         append([]ResourceLink(nil), record.Resources...),
		Gaps:              append([]GapLink(nil), record.Gaps...),
		Alerts:            append([]AlertCaseLink(nil), record.Alerts...),
		Annotations:       append([]Annotation(nil), record.Annotations...),
	}
	if len(pack.Resources) > 0 {
		pack.SchemaVersion = ResourceEvidencePackSchema
		pack.ContentBoundary += "; immutable resource revision identities and server-resolved synthetic metadata, without independent content verification"
	}
	if pack.Responsibility == nil {
		pack.Responsibility = &FollowupSnapshot{Followups: []Followup{}, Actions: []FollowupAction{}}
		for _, finding := range record.Findings {
			pack.Responsibility.Followups = append(pack.Responsibility.Followups, Followup{CaseID: record.Case.ID, FindingID: finding.ID})
		}
	} else {
		pack.Responsibility = &FollowupSnapshot{
			Followups: append([]Followup{}, record.Responsibility.Followups...),
			Actions:   append([]FollowupAction{}, record.Responsibility.Actions...),
		}
	}
	if pack.Assessments == nil {
		pack.Assessments = &AssessmentSnapshot{Assessments: []Assessment{}, Actions: []AssessmentAction{}, Creations: []ReviewCreationReceipt{}}
		for _, finding := range record.Findings {
			pack.Assessments.Assessments = append(pack.Assessments.Assessments, Assessment{CaseID: record.Case.ID, FindingID: finding.ID})
		}
	} else {
		pack.Assessments = &AssessmentSnapshot{Assessments: append([]Assessment{}, record.Assessments.Assessments...), Actions: append([]AssessmentAction{}, record.Assessments.Actions...), Creations: append([]ReviewCreationReceipt{}, record.Assessments.Creations...)}
	}
	if packHasContent(pack) {
		pack.SchemaVersion = ContentEvidencePackSchema
		pack.ContentBoundary += "; immutable content references and byte/JSON locations only; source content bytes are not included"
		if err := validatePackContentLinks(pack); err != nil {
			return EvidencePack{}, nil, "", err
		}
	}
	if err := ValidateExecutionContextPack(pack); err != nil {
		return EvidencePack{}, nil, "", err
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
	sort.Slice(pack.Contents, func(i, j int) bool {
		a, b := pack.Contents[i], pack.Contents[j]
		if a.FindingID != b.FindingID {
			return a.FindingID < b.FindingID
		}
		return ContentChoiceKey(a.Reference) < ContentChoiceKey(b.Reference)
	})
	sort.Slice(pack.Resources, func(i, j int) bool {
		a, b := pack.Resources[i], pack.Resources[j]
		if a.FindingID != b.FindingID {
			return a.FindingID < b.FindingID
		}
		return ResourceChoiceKey(a.Reference) < ResourceChoiceKey(b.Reference)
	})
	if pack.Assessments != nil {
		sort.Slice(pack.Assessments.Assessments, func(i, j int) bool {
			return pack.Assessments.Assessments[i].FindingID < pack.Assessments.Assessments[j].FindingID
		})
		sort.Slice(pack.Assessments.Actions, func(i, j int) bool {
			a, b := pack.Assessments.Actions[i], pack.Assessments.Actions[j]
			if a.Current.FindingID == b.Current.FindingID {
				return a.Current.Version < b.Current.Version
			}
			return a.Current.FindingID < b.Current.FindingID
		})
		sort.Slice(pack.Assessments.Creations, func(i, j int) bool {
			return pack.Assessments.Creations[i].FindingID < pack.Assessments.Creations[j].FindingID
		})
	}
	if pack.Responsibility != nil {
		sort.Slice(pack.Responsibility.Followups, func(i, j int) bool {
			return pack.Responsibility.Followups[i].FindingID < pack.Responsibility.Followups[j].FindingID
		})
		sort.Slice(pack.Responsibility.Actions, func(i, j int) bool {
			a, b := pack.Responsibility.Actions[i], pack.Responsibility.Actions[j]
			if a.Current.FindingID == b.Current.FindingID {
				return a.Current.Version < b.Current.Version
			}
			return a.Current.FindingID < b.Current.FindingID
		})
	}
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
	return RenderEvidencePackHTMLInLanguage(pack, digest, "en-US")
}

// RenderEvidencePackHTMLInLanguage changes presentation only. Sealed JSON and its digest are untouched.
func RenderEvidencePackHTMLInLanguage(pack EvidencePack, digest, language string) ([]byte, error) {
	if language != "zh-CN" {
		language = "en-US"
	}
	if !SupportedEvidencePack(pack) {
		return nil, ErrInvalid
	}
	if !hashPattern.MatchString(digest) {
		return nil, ErrInvalid
	}
	var output bytes.Buffer
	if err := evidencePackTemplates[language].Execute(&output, struct {
		Language string
		Pack     EvidencePack
		Digest   string
	}{Language: language, Pack: pack, Digest: digest}); err != nil {
		return nil, fmt.Errorf("render Evidence Pack: %w", err)
	}
	return output.Bytes(), nil
}

func SupportedEvidencePack(pack EvidencePack) bool {
	if ValidateExecutionContextPack(pack) != nil {
		return false
	}
	if packHasContent(pack) != (pack.SchemaVersion == ContentEvidencePackSchema) {
		return false
	}
	if validatePackContentLinks(pack) != nil {
		return false
	}
	if pack.SchemaVersion == EvidencePackSchemaV1 {
		return len(pack.Resources) == 0 && pack.Responsibility == nil && pack.Assessments == nil
	}
	if pack.Responsibility == nil || pack.Responsibility.Followups == nil || pack.Responsibility.Actions == nil {
		return false
	}
	if pack.SchemaVersion == EvidencePackSchemaV2 {
		return len(pack.Resources) == 0 && pack.Assessments == nil
	}
	return (pack.SchemaVersion == EvidencePackSchema || pack.SchemaVersion == ResourceEvidencePackSchema || pack.SchemaVersion == ContentEvidencePackSchema) && (pack.SchemaVersion == ResourceEvidencePackSchema || pack.SchemaVersion == ContentEvidencePackSchema || len(pack.Resources) == 0) && pack.Assessments != nil && pack.Assessments.Assessments != nil && pack.Assessments.Actions != nil && pack.Assessments.Creations != nil
}

const evidencePackTemplateSource = `<!doctype html>
<html lang="{{.Language}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Pack.Title}} {{tr "p000"}}</title>
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
<p>{{tr "p001"}}</p>
<h1>{{.Pack.Title}}</h1>
<p>{{.Pack.Description}}</p>
<div class="meta">
<span>{{tr "p002"}} <code>{{.Pack.CaseID}}</code></span>
<span>{{tr "p003"}} {{.Pack.Revision}}</span>
<span>{{tr "p004"}} <time>{{.Pack.SealedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></span>
<span>{{tr "p005"}} <code>{{.Digest}}</code></span>
<span>{{tr "p006"}} <code>{{.Pack.SealedBySubject}}</code></span>
<span>{{tr "p007"}} <code>{{.Pack.RedactionPolicy}}</code></span>
</div>
</header>
<main>
{{define "review-basis"}}<details><summary>{{tr "p008"}} {{.FindingVersion}}</summary><p>{{.Statement}}</p>{{with .ResolvedExecutionContext}}<p>{{tr "p009"}}<code>{{.Context.ExecutionID}}</code></p><p>{{tr "p010"}}{{.Context.ExecutionAsOf}}；{{if .Contents}}{{tr "p011"}}{{else}}{{tr "p012"}}{{end}}{{tr "p013"}}{{.RuntimeCutoff}}。</p>{{end}}<code>{{.Digest}}</code><p>{{tr "p014"}}{{len .Known.Evidence}} {{tr "p015"}} {{len .Known.Gaps}} {{tr "p016"}}{{len .Selected.Evidence}} {{tr "p015"}} {{len .Selected.Gaps}} {{tr "p017"}}</p>{{range .Selected.Evidence}}<p>{{.Scope}} · {{.Relation}} · <code>{{.EvidenceEventID}}</code><br><code>{{.EventHash}}</code><br>{{.SourceRef}}</p>{{end}}{{range .Selected.Contents}}<article class="item"><p>{{tr "p018"}} {{.Relation}} · {{.Reference.CaptureStatus}}</p><p>{{.Reference.SourceSystemID}} / {{.Reference.SourceInstance}} / {{.Reference.OrgID}} / {{.Reference.ProjectID}} / {{.Reference.RequestID}}</p><p>{{.Reference.SourceFactID}} · {{.Reference.Revision}} · <code>{{.Reference.SourceSHA256}}</code></p><p>{{tr "p019"}} {{.Reference.ContentRef}} · {{.Location.Kind}}{{tr "p020"}}</p></article>{{end}}{{range .AdoptedResources}}<article class="item"><p>{{tr "p021"}}{{.DecodedRecord.Resource.Name}} · {{.Reference.Relation}} {{tr "p022"}}</p><p>{{.Reference.Reporter.SourceSystemID}} / {{.Reference.Reporter.SourceInstance}} / {{.Reference.Reporter.OrgID}} / {{.Reference.Reporter.ProjectID}}</p><p>{{.Reference.RecordRef}} {{tr "p023"}} {{.Reference.Revision}} · <code>{{.Reference.RecordHash}}</code></p><p>{{tr "p024"}}{{.Reference.Target.SourceSystemID}} / {{.Reference.Target.SourceInstance}} / {{.Reference.Target.OrgID}} / {{.Reference.Target.ProjectID}} / {{.Reference.RequestID}}</p><p>{{tr "p025"}}{{.DecodedRecord.Withdrawn}}{{tr "p026"}}</p></article>{{end}}{{range .AdoptedGaps}}<article class="item gap"><p>{{.Reference.Scope}} · {{.Reference.Relation}} · {{.Reference.GapType}}</p><p>{{.Reference.AffectedClaim}}</p><code>{{.Reference.GapID}}</code>{{if .ResolutionRecorded}}<p>{{tr "p027"}} {{.ResolutionVersion}} · {{.State}}</p><p>{{.Note}}</p><p>{{.UpdatedBySubject}} · {{.UpdatedAt}}</p>{{else}}<p>{{tr "p028"}}</p>{{end}}</article>{{end}}</details>{{end}}
{{if .Pack.Assessments}}<section><h2>{{tr "p029"}}</h2><p>{{tr "p030"}}</p>
{{range .Pack.Assessments.Assessments}}<article class="item"><code>{{.FindingID}}</code>{{if .Review}}<p>{{tr "p031"}}{{.Review.Conclusion}} · {{.Review.ActorSubjectID}} · {{.Review.At}}</p><p>{{.Review.Reason}}</p>{{if .Review.MissingEvidence}}<p>{{tr "p032"}}{{.Review.MissingEvidence}}</p>{{end}}{{template "review-basis" .Review.Basis}}{{else}}<p>{{tr "p033"}}</p>{{end}}{{if .Progress}}<p>{{tr "p034"}}{{.Progress.State}} · {{.Progress.ActorSubjectID}} · {{.Progress.At}}</p><p>{{.Progress.Reason}}</p>{{if .Progress.Closure}}<p>{{tr "p035"}}{{.Progress.Closure.Kind}}</p><p>{{.Progress.Closure.Explanation}}</p>{{if .Progress.Closure.Scope}}<p>{{tr "p036"}}{{.Progress.Closure.Scope}} {{tr "p037"}} {{.Progress.Closure.VerifiedAt}}</p><p>{{tr "p038"}}</p>{{range .Progress.Closure.VerificationResources}}<p>{{tr "p039"}}{{.RecordRef}} · {{.Revision}} · {{.RecordHash}}</p>{{end}}{{range .Progress.Closure.VerificationEvidence}}<p>{{tr "p040"}}<code>{{.EvidenceEventID}}</code> · {{.Relation}} · {{.SourceRef}}</p>{{end}}{{end}}{{if .Progress.Closure.Duplicate}}<p>{{tr "p041"}}<code>{{.Progress.Closure.Duplicate.CaseID}} / {{.Progress.Closure.Duplicate.FindingID}}</code></p>{{end}}{{if .Progress.Closure.RemainingUnknowns}}<p>{{tr "p042"}}{{.Progress.Closure.RemainingUnknowns}}</p>{{end}}{{end}}{{template "review-basis" .Progress.Basis}}{{else}}<p>{{tr "p043"}}</p>{{end}}</article>{{end}}
<h3>{{tr "p044"}}</h3>{{range .Pack.Assessments.Actions}}<article class="item"><code>{{.Current.FindingID}}</code><p>{{.Operation}} {{tr "p045"}} {{.Previous.Version}} → {{.Current.Version}} {{tr "p046"}} {{.CaseVersion}}</p><p>{{.ActorSubjectID}} · {{.At}}</p>{{if .Command.Review}}<p>{{.Command.Review.Conclusion}} · {{.Command.Review.Reason}}</p><p>{{.Command.Review.MissingEvidence}}</p>{{end}}{{if .Command.Progress}}<p>{{.Command.Progress.State}} · {{.Command.Progress.Reason}}</p>{{if .Command.Progress.Closure}}<p>{{.Command.Progress.Closure.Kind}} · {{.Command.Progress.Closure.Explanation}}</p><p>{{.Command.Progress.Closure.Scope}} · {{.Command.Progress.Closure.VerifiedAt}}</p><p>{{.Command.Progress.Closure.RemainingUnknowns}}</p>{{if .Command.Progress.Closure.Duplicate}}<p>{{.Command.Progress.Closure.Duplicate.CaseID}} / {{.Command.Progress.Closure.Duplicate.FindingID}}</p>{{end}}{{range .Command.Progress.Closure.VerificationResources}}<p>{{tr "p039"}}{{.RecordRef}} · {{.Revision}} · {{.RecordHash}}</p>{{end}}{{range .Command.Progress.Closure.VerificationEvidence}}<p>{{tr "p047"}}{{.EvidenceEventID}} · {{.SourceRef}}</p>{{end}}{{end}}{{end}}{{template "review-basis" .Basis}}<code>{{.OperationID}}</code></article>{{else}}<p>{{tr "p048"}}</p>{{end}}
<h3>{{tr "p049"}}</h3>{{range .Pack.Assessments.Creations}}<article class="item"><code>{{.FindingID}}</code><p>{{.Operation}} · {{.ActorSubjectID}} · {{.At}}</p><p>{{.Command.Reason}}</p>{{if .Command.From}}<p>{{tr "p050"}}{{.Command.From.CaseID}} / {{.Command.From.FindingID}} {{tr "p051"}} {{.Command.From.Revision}}</p><code>{{.Command.From.Digest}}</code><p>{{tr "p052"}}</p>{{end}}<code>{{.OperationID}}</code></article>{{else}}<p>{{tr "p053"}}</p>{{end}}</section>{{end}}
{{if .Pack.Responsibility}}<section><h2>{{tr "p054"}}</h2><p>{{tr "p055"}}</p>{{range .Pack.Responsibility.Followups}}<article class="item"><code>{{.FindingID}}</code>{{if .Recorded}}<p>{{tr "p056"}} {{.Version}} · {{if .AssigneeSubjectID}}{{.AssigneeName}} · <code>{{.AssigneeSubjectID}}</code>{{else}}{{tr "p057"}}{{end}}</p><p>{{if .DueAt}}{{tr "p058"}} {{.DueAt}} · {{.DueTimezone}}{{else}}{{tr "p059"}}{{end}}</p>{{else}}<p>{{tr "p060"}}</p>{{end}}</article>{{end}}<h3>{{tr "p061"}}</h3>{{range .Pack.Responsibility.Actions}}<article class="item"><code>{{.Current.FindingID}}</code><p>{{tr "p062"}} {{.ExpectedVersion}} → {{.Current.Version}} · {{.At}} · {{.ActorSubjectID}}</p><p>{{.Reason}}</p><p>{{tr "p063"}}{{if .Previous.Recorded}}{{if .Previous.AssigneeSubjectID}}{{.Previous.AssigneeName}} · {{.Previous.AssigneeSubjectID}}{{else}}{{tr "p057"}}{{end}}{{else}}{{tr "p064"}}{{end}} → {{if .Current.AssigneeSubjectID}}{{.Current.AssigneeName}} · {{.Current.AssigneeSubjectID}}{{else}}{{tr "p057"}}{{end}}</p><p>{{tr "p065"}}{{if .Previous.DueAt}}{{.Previous.DueAt}} · {{.Previous.DueTimezone}}{{else}}{{tr "p066"}}{{end}} → {{if .Current.DueAt}}{{.Current.DueAt}} · {{.Current.DueTimezone}}{{else}}{{tr "p066"}}{{end}}</p><code>{{tr "p067"}} {{.OperationID}} {{tr "p068"}} {{.CaseVersion}}</code></article>{{else}}<p>{{tr "p069"}}</p>{{end}}</section>{{end}}
<section><h2>{{tr "p070"}}</h2><p><code>{{.Pack.RedactionPolicy}}</code></p><p>{{.Pack.ContentBoundary}}</p><p><strong>{{tr "p071"}}</strong> {{.Pack.UserContentPolicy}}</p><p>{{tr "p072"}}</p></section>
{{if .Pack.Contents}}<section><h2>{{tr "p073"}}</h2>{{range .Pack.Contents}}<article class="item"><p>{{.Reference.Relation}} · {{.Reference.Reference.SourceFactID}} {{tr "p023"}} {{.Reference.Reference.Revision}}</p><code>{{.Reference.Reference.SourceSHA256}}</code><p>{{.Reference.Location.Kind}}{{tr "p074"}}</p></article>{{end}}</section>{{end}}<section><h2>{{tr "p075"}}</h2>{{range .Pack.Findings}}<article class="item"><h3>{{.Title}}</h3><p>{{.Statement}}</p><p><code>{{.Classification}}</code> · <code>{{.Status}}</code></p></article>{{else}}<p>{{tr "p076"}}</p>{{end}}{{range .Pack.Annotations}}<article class="item"><p>{{.Body}}</p><code>{{tr "p077"}} {{.TargetKind}} · {{.TargetRef}}</code></article>{{end}}</section>
<section><h2>{{tr "p078"}}</h2>{{range .Pack.Executions}}<article class="item"><code>{{.ExecutionID}}</code><br><time>{{.AsOf.Format "2006-01-02T15:04:05.000000Z07:00"}}</time><p>{{tr "p079"}}</p></article>{{else}}<p>{{tr "p080"}}</p>{{end}}{{range .Pack.Alerts}}<article class="item"><code>{{.AlertID}}</code><p>{{tr "p081"}} <code>{{.AlertState}}</code></p></article>{{else}}<p>{{tr "p082"}}</p>{{end}}</section>
	<section><h2>{{tr "p083"}}</h2>{{range .Pack.Gaps}}<article class="item gap"><strong>{{.GapTypeSnapshot}}</strong><p>{{.AffectedClaimSnapshot}}</p><code>{{.GapID}}</code><p>{{.Relation}}</p>{{if .FindingID}}<p>{{tr "p084"}} <code>{{.FindingID}}</code></p>{{else}}<p>{{tr "p085"}}</p>{{end}}</article>{{else}}<p>{{tr "p086"}}</p>{{end}}</section>
	<section><h2>{{tr "p087"}}</h2>{{if .Pack.Resources}}<h2>{{tr "p088"}}</h2>{{range .Pack.Resources}}<article class="item"><p>{{.FindingID}} · {{.Reference.Relation}} · {{.Reference.RecordRef}} / {{.Reference.Revision}}</p><p>{{tr "p089"}}{{.Reference.Reporter.SourceSystemID}} / {{.Reference.Reporter.SourceInstance}} / {{.Reference.Reporter.OrgID}} / {{.Reference.Reporter.ProjectID}}</p><p>{{tr "p024"}}{{.Reference.Target.SourceSystemID}} / {{.Reference.Target.SourceInstance}} / {{.Reference.Target.OrgID}} / {{.Reference.Target.ProjectID}} / {{.Reference.RequestID}}</p><code>{{.Reference.RecordHash}}</code><p>{{.AddedBySubject}} · {{.AddedAt}}</p></article>{{end}}{{end}}{{range .Pack.Evidence}}<article class="item"><code>{{.EvidenceEventID}}</code><br><code>{{.EventHashSnapshot}}</code><br><code>{{.SourceRefSnapshot}}</code><p>{{.Relation}}</p>{{if .FindingID}}<p>{{tr "p084"}} <code>{{.FindingID}}</code></p>{{else}}<p>{{tr "p090"}}</p>{{end}}</article>{{else}}<p>{{tr "p091"}}</p>{{end}}</section>
<section><h2>{{tr "p092"}}</h2>{{range .Pack.Executions}}<article class="item"><code>{{.ExecutionID}}</code><p>{{tr "p093"}} <code>{{.AddedBySubject}}</code> {{tr "p094"}} <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Findings}}<article class="item"><code>{{.ID}}</code><p>{{tr "p095"}} <code>{{.CreatedBySubject}}</code> {{tr "p094"}} <time>{{.CreatedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Evidence}}<article class="item"><code>{{.EvidenceEventID}}</code><p>{{tr "p093"}} <code>{{.AddedBySubject}}</code> {{tr "p094"}} <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Gaps}}<article class="item"><code>{{.GapID}}</code><p>{{tr "p093"}} <code>{{.AddedBySubject}}</code> {{tr "p094"}} <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Alerts}}<article class="item"><code>{{.AlertID}}</code><p>{{tr "p093"}} <code>{{.LinkedBySubject}}</code> {{tr "p094"}} <time>{{.LinkedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}</section>
<section><h2>{{tr "p096"}}</h2><p><code>{{.Digest}}</code></p><p>{{tr "p097"}}</p></section>
</main>
</body>
</html>`
