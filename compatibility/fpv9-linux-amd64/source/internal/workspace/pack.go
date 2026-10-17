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
	if !SupportedEvidencePack(pack) {
		return nil, ErrInvalid
	}
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
{{define "review-basis"}}<details><summary>当时采用的依据 · Finding 版本 {{.FindingVersion}}</summary><p>{{.Statement}}</p>{{with .ResolvedExecutionContext}}<p>关联执行：<code>{{.Context.ExecutionID}}</code></p><p>原执行阅读截止：{{.Context.ExecutionAsOf}}；{{if .Contents}}相关资料{{else}}资源{{end}}阅读截止：{{.RuntimeCutoff}}。</p>{{end}}<code>{{.Digest}}</code><p>当时已知 Finding 引用：{{len .Known.Evidence}} Evidence / {{len .Known.Gaps}} Gap；本次实际采用：{{len .Selected.Evidence}} Evidence / {{len .Selected.Gaps}} Gap。</p>{{range .Selected.Evidence}}<p>{{.Scope}} · {{.Relation}} · <code>{{.EvidenceEventID}}</code><br><code>{{.EventHash}}</code><br>{{.SourceRef}}</p>{{end}}{{range .Selected.Contents}}<article class="item"><p>内容引用 · {{.Relation}} · {{.Reference.CaptureStatus}}</p><p>{{.Reference.SourceSystemID}} / {{.Reference.SourceInstance}} / {{.Reference.OrgID}} / {{.Reference.ProjectID}} / {{.Reference.RequestID}}</p><p>{{.Reference.SourceFactID}} · {{.Reference.Revision}} · <code>{{.Reference.SourceSHA256}}</code></p><p>保存表示 {{.Reference.ContentRef}} · {{.Location.Kind}}；正文未包含在封存包中。</p></article>{{end}}{{range .AdoptedResources}}<article class="item"><p>资源引用：{{.DecodedRecord.Resource.Name}} · {{.Reference.Relation}} · 合成声明</p><p>{{.Reference.Reporter.SourceSystemID}} / {{.Reference.Reporter.SourceInstance}} / {{.Reference.Reporter.OrgID}} / {{.Reference.Reporter.ProjectID}}</p><p>{{.Reference.RecordRef}} · 修订 {{.Reference.Revision}} · <code>{{.Reference.RecordHash}}</code></p><p>目标：{{.Reference.Target.SourceSystemID}} / {{.Reference.Target.SourceInstance}} / {{.Reference.Target.OrgID}} / {{.Reference.Target.ProjectID}} / {{.Reference.RequestID}}</p><p>当时记录撤回标记：{{.DecodedRecord.Withdrawn}}；未证明实际内容或外发许可。</p></article>{{end}}{{range .AdoptedGaps}}<article class="item gap"><p>{{.Reference.Scope}} · {{.Reference.Relation}} · {{.Reference.GapType}}</p><p>{{.Reference.AffectedClaim}}</p><code>{{.Reference.GapID}}</code>{{if .ResolutionRecorded}}<p>当时解决记录版本 {{.ResolutionVersion}} · {{.State}}</p><p>{{.Note}}</p><p>{{.UpdatedBySubject}} · {{.UpdatedAt}}</p>{{else}}<p>当时没有解决记录；不推定为 open。</p>{{end}}</article>{{end}}</details>{{end}}
{{if .Pack.Assessments}}<section><h2>人工复核与跟进 · 封存时记录</h2><p>结论与进度为人工记录，独立于源事实和责任。supported 表示证据支持；not_supported 表示证据不支持；insufficient_evidence 表示证据不足；pending_review 表示待复核。跟进结束不自动表示风险消除。</p>
{{range .Pack.Assessments.Assessments}}<article class="item"><code>{{.FindingID}}</code>{{if .Review}}<p>结论：{{.Review.Conclusion}} · {{.Review.ActorSubjectID}} · {{.Review.At}}</p><p>{{.Review.Reason}}</p>{{if .Review.MissingEvidence}}<p>缺少：{{.Review.MissingEvidence}}</p>{{end}}{{template "review-basis" .Review.Basis}}{{else}}<p>复核未记录</p>{{end}}{{if .Progress}}<p>跟进：{{.Progress.State}} · {{.Progress.ActorSubjectID}} · {{.Progress.At}}</p><p>{{.Progress.Reason}}</p>{{if .Progress.Closure}}<p>结束类型：{{.Progress.Closure.Kind}}</p><p>{{.Progress.Closure.Explanation}}</p>{{if .Progress.Closure.Scope}}<p>人工核验范围：{{.Progress.Closure.Scope}} · 核验时间 {{.Progress.Closure.VerifiedAt}}</p><p>这是人工核验记录，不是 XI 执行修复的证明。</p>{{range .Progress.Closure.VerificationResources}}<p>人工核验资源：{{.RecordRef}} · {{.Revision}} · {{.RecordHash}}</p>{{end}}{{range .Progress.Closure.VerificationEvidence}}<p>修复后核验引用：<code>{{.EvidenceEventID}}</code> · {{.Relation}} · {{.SourceRef}}</p>{{end}}{{end}}{{if .Progress.Closure.Duplicate}}<p>重复并入：<code>{{.Progress.Closure.Duplicate.CaseID}} / {{.Progress.Closure.Duplicate.FindingID}}</code></p>{{end}}{{if .Progress.Closure.RemainingUnknowns}}<p>剩余未知：{{.Progress.Closure.RemainingUnknowns}}</p>{{end}}{{end}}{{template "review-basis" .Progress.Basis}}{{else}}<p>跟进进度未记录</p>{{end}}</article>{{end}}
<h3>完整复核与进度历史</h3>{{range .Pack.Assessments.Actions}}<article class="item"><code>{{.Current.FindingID}}</code><p>{{.Operation}} · 版本 {{.Previous.Version}} → {{.Current.Version}} · Case 版本 {{.CaseVersion}}</p><p>{{.ActorSubjectID}} · {{.At}}</p>{{if .Command.Review}}<p>{{.Command.Review.Conclusion}} · {{.Command.Review.Reason}}</p><p>{{.Command.Review.MissingEvidence}}</p>{{end}}{{if .Command.Progress}}<p>{{.Command.Progress.State}} · {{.Command.Progress.Reason}}</p>{{if .Command.Progress.Closure}}<p>{{.Command.Progress.Closure.Kind}} · {{.Command.Progress.Closure.Explanation}}</p><p>{{.Command.Progress.Closure.Scope}} · {{.Command.Progress.Closure.VerifiedAt}}</p><p>{{.Command.Progress.Closure.RemainingUnknowns}}</p>{{if .Command.Progress.Closure.Duplicate}}<p>{{.Command.Progress.Closure.Duplicate.CaseID}} / {{.Command.Progress.Closure.Duplicate.FindingID}}</p>{{end}}{{range .Command.Progress.Closure.VerificationResources}}<p>人工核验资源：{{.RecordRef}} · {{.Revision}} · {{.RecordHash}}</p>{{end}}{{range .Command.Progress.Closure.VerificationEvidence}}<p>人工核验引用：{{.EvidenceEventID}} · {{.SourceRef}}</p>{{end}}{{end}}{{end}}{{template "review-basis" .Basis}}<code>{{.OperationID}}</code></article>{{else}}<p>尚无复核或进度动作。</p>{{end}}
<h3>首次创建与封存续办来源</h3>{{range .Pack.Assessments.Creations}}<article class="item"><code>{{.FindingID}}</code><p>{{.Operation}} · {{.ActorSubjectID}} · {{.At}}</p><p>{{.Command.Reason}}</p>{{if .Command.From}}<p>原封存记录：{{.Command.From.CaseID}} / {{.Command.From.FindingID}} · revision {{.Command.From.Revision}}</p><code>{{.Command.From.Digest}}</code><p>该引用没有把原判断复制为新结论或授予原负责人权限。</p>{{end}}<code>{{.OperationID}}</code></article>{{else}}<p>无组合创建或续办来源。</p>{{end}}</section>{{end}}
{{if .Pack.Responsibility}}<section><h2>责任与期限 · 封存时记录</h2><p>人工责任记录不授予访问权，也不证明复核结论、风险消除或源控制已执行。</p>{{range .Pack.Responsibility.Followups}}<article class="item"><code>{{.FindingID}}</code>{{if .Recorded}}<p>责任版本 {{.Version}} · {{if .AssigneeSubjectID}}{{.AssigneeName}} · <code>{{.AssigneeSubjectID}}</code>{{else}}未分配{{end}}</p><p>{{if .DueAt}}登记期限 {{.DueAt}} · {{.DueTimezone}}{{else}}未设期限{{end}}</p>{{else}}<p>责任信息未记录</p>{{end}}</article>{{end}}<h3>完整修改历史</h3>{{range .Pack.Responsibility.Actions}}<article class="item"><code>{{.Current.FindingID}}</code><p>版本 {{.ExpectedVersion}} → {{.Current.Version}} · {{.At}} · {{.ActorSubjectID}}</p><p>{{.Reason}}</p><p>原负责人：{{if .Previous.Recorded}}{{if .Previous.AssigneeSubjectID}}{{.Previous.AssigneeName}} · {{.Previous.AssigneeSubjectID}}{{else}}未分配{{end}}{{else}}未记录{{end}} → {{if .Current.AssigneeSubjectID}}{{.Current.AssigneeName}} · {{.Current.AssigneeSubjectID}}{{else}}未分配{{end}}</p><p>原期限：{{if .Previous.DueAt}}{{.Previous.DueAt}} · {{.Previous.DueTimezone}}{{else}}未设置{{end}} → {{if .Current.DueAt}}{{.Current.DueAt}} · {{.Current.DueTimezone}}{{else}}未设置{{end}}</p><code>operation {{.OperationID}} · Case {{.CaseVersion}}</code></article>{{else}}<p>尚无责任修改记录。</p>{{end}}</section>{{end}}
<section><h2>Scope and redaction</h2><p><code>{{.Pack.RedactionPolicy}}</code></p><p>{{.Pack.ContentBoundary}}</p><p><strong>User content:</strong> {{.Pack.UserContentPolicy}}</p><p>This dossier describes one sealed Case revision. Absence of a linked Gap does not establish global source completeness.</p></section>
{{if .Pack.Contents}}<section><h2>内容引用</h2>{{range .Pack.Contents}}<article class="item"><p>{{.Reference.Relation}} · {{.Reference.Reference.SourceFactID}} · 修订 {{.Reference.Reference.Revision}}</p><code>{{.Reference.Reference.SourceSHA256}}</code><p>{{.Reference.Location.Kind}}；保留引用，不包含正文。</p></article>{{end}}</section>{{end}}<section><h2>Findings</h2>{{range .Pack.Findings}}<article class="item"><h3>{{.Title}}</h3><p>{{.Statement}}</p><p><code>{{.Classification}}</code> · <code>{{.Status}}</code></p></article>{{else}}<p>No findings recorded.</p>{{end}}{{range .Pack.Annotations}}<article class="item"><p>{{.Body}}</p><code>annotation · {{.TargetKind}} · {{.TargetRef}}</code></article>{{end}}</section>
<section><h2>Execution stories and Alerts</h2>{{range .Pack.Executions}}<article class="item"><code>{{.ExecutionID}}</code><br><time>{{.AsOf.Format "2006-01-02T15:04:05.000000Z07:00"}}</time><p>Story remains addressable at this fixed Evidence snapshot.</p></article>{{else}}<p>No executions linked.</p>{{end}}{{range .Pack.Alerts}}<article class="item"><code>{{.AlertID}}</code><p>Source state at seal: <code>{{.AlertState}}</code></p></article>{{else}}<p>No source Alerts linked.</p>{{end}}</section>
	<section><h2>Completeness and Capture Gaps</h2>{{range .Pack.Gaps}}<article class="item gap"><strong>{{.GapTypeSnapshot}}</strong><p>{{.AffectedClaimSnapshot}}</p><code>{{.GapID}}</code><p>{{.Relation}}</p>{{if .FindingID}}<p>Finding <code>{{.FindingID}}</code></p>{{else}}<p>Case-level limitation</p>{{end}}</article>{{else}}<p>No Capture Gaps are linked to this Case. This does not prove global completeness.</p>{{end}}</section>
	<section><h2>Evidence citations</h2>{{if .Pack.Resources}}<h2>资源修订引用</h2>{{range .Pack.Resources}}<article class="item"><p>{{.FindingID}} · {{.Reference.Relation}} · {{.Reference.RecordRef}} / {{.Reference.Revision}}</p><p>报告方：{{.Reference.Reporter.SourceSystemID}} / {{.Reference.Reporter.SourceInstance}} / {{.Reference.Reporter.OrgID}} / {{.Reference.Reporter.ProjectID}}</p><p>目标：{{.Reference.Target.SourceSystemID}} / {{.Reference.Target.SourceInstance}} / {{.Reference.Target.OrgID}} / {{.Reference.Target.ProjectID}} / {{.Reference.RequestID}}</p><code>{{.Reference.RecordHash}}</code><p>{{.AddedBySubject}} · {{.AddedAt}}</p></article>{{end}}{{end}}{{range .Pack.Evidence}}<article class="item"><code>{{.EvidenceEventID}}</code><br><code>{{.EventHashSnapshot}}</code><br><code>{{.SourceRefSnapshot}}</code><p>{{.Relation}}</p>{{if .FindingID}}<p>Finding <code>{{.FindingID}}</code></p>{{else}}<p>Case-level citation</p>{{end}}</article>{{else}}<p>No Evidence linked.</p>{{end}}</section>
<section><h2>Provenance appendix</h2>{{range .Pack.Executions}}<article class="item"><code>{{.ExecutionID}}</code><p>linked by <code>{{.AddedBySubject}}</code> at <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Findings}}<article class="item"><code>{{.ID}}</code><p>created by <code>{{.CreatedBySubject}}</code> at <time>{{.CreatedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Evidence}}<article class="item"><code>{{.EvidenceEventID}}</code><p>linked by <code>{{.AddedBySubject}}</code> at <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Gaps}}<article class="item"><code>{{.GapID}}</code><p>linked by <code>{{.AddedBySubject}}</code> at <time>{{.AddedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}{{range .Pack.Alerts}}<article class="item"><code>{{.AlertID}}</code><p>linked by <code>{{.LinkedBySubject}}</code> at <time>{{.LinkedAt.Format "2006-01-02T15:04:05.000000Z07:00"}}</time></p></article>{{end}}</section>
<section><h2>Verification digest</h2><p><code>{{.Digest}}</code></p><p>Algorithm: SHA-256 over the exact compact JSON download bytes for this sealed revision. The HTML presentation is not part of that digest.</p></section>
</main>
</body>
</html>`))
