package db

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

type queueCursor struct {
	Schema  int       `json:"schema"`
	Subject string    `json:"subject"`
	Request string    `json:"request"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
	After   string    `json:"after"`
}
type queueFinding struct {
	item       workspace.QueueItem
	assessment workspace.Assessment
}
type queueEvidence struct {
	link      workspace.EvidenceChoice
	hash, ref string
}
type queueGap struct {
	link workspace.GapChoice
	row  selectedGapRow
}
type queueBasisRows struct {
	contents        map[string]workspace.ContentChoice
	resourceUpdates map[string]workspace.ResourceUpdate
	resources       map[string]workspace.AdoptedResource
	known           map[string]workspace.BasisSelection
	evidence        map[string]queueEvidence
	gaps            map[string]queueGap
}

func queueLinkKey(caseID, findingID, key string) string { return caseID + "/" + findingID + "/" + key }

// The current deployment grants Case readers the whole investigation scope.
// API authentication supplies this access binding; request filters never grant
// access, and a future project ACL must enter before this candidate query.
func (s *WorkspaceStore) ListInvestigationQueue(ctx context.Context, access workspace.QueueAccess, request workspace.QueueRequest, now time.Time) (workspace.QueuePage, error) {
	result := workspace.QueuePage{Items: []workspace.QueueItem{}, Coverage: "retained_investigation_records; security_source_coverage_unconfirmed"}
	if _, err := workspace.HumanActor(access.Subject); err != nil || access.Revision == "" {
		return result, workspace.ErrInvalid
	}
	if a := workchain.FromContext(ctx); a != nil {
		access.Revision = assessmentRequestHash([]string{access.Revision, a.Revision})
	}
	filters, err := workspace.NormalizeQueueFilters(request.Filters)
	if err != nil {
		return result, err
	}
	result.Filters = filters
	if request.Limit < 1 || request.Limit > 100 {
		return result, workspace.ErrInvalid
	}
	fingerprint := assessmentRequestHash(struct {
		Filters workspace.QueueFilters
		Access  string
	}{filters, access.Revision})
	cursor := queueCursor{Schema: 1, Subject: access.Subject, Request: fingerprint, At: now.UTC()}
	if request.Cursor != "" {
		if len(request.Cursor) > 4096 {
			return result, workspace.ErrInvalid
		}
		raw, e := base64.RawURLEncoding.DecodeString(request.Cursor)
		if e != nil {
			return result, workspace.ErrInvalid
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if e = decoder.Decode(&cursor); e != nil || cursor.Schema != 1 || cursor.Subject != access.Subject || cursor.Request != fingerprint || cursor.At.IsZero() || cursor.At.After(now) || cursor.After == "" || len(cursor.Version) != 64 {
			return result, workspace.ErrInvalid
		}
		if decoder.Decode(new(any)) != io.EOF {
			return result, workspace.ErrInvalid
		}
		if now.Sub(cursor.At) > 30*time.Minute {
			return result, workspace.ErrConflict
		}
	}
	result.EvaluatedAt = cursor.At.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	condition, err := unscopedContentCaseCondition(ctx, tx, "c.id")
	if err != nil {
		return result, err
	}
	args := []any{}
	if a := workchain.FromContext(ctx); a != nil {
		condition = scopedCaseSQL("c.id", "$1")
		args = append(args, a.Subject)
	}
	rows, err := tx.QueryContext(ctx, `SELECT to_jsonb(f),c.title,c.status,a.record,to_jsonb(u) FROM case_findings f JOIN investigation_cases c ON c.id=f.case_id LEFT JOIN case_finding_assessments a ON a.case_id=f.case_id AND a.finding_id=f.id LEFT JOIN case_finding_followups u ON u.case_id=f.case_id AND u.finding_id=f.id WHERE `+condition+` ORDER BY f.id`, args...)
	if err != nil {
		return result, err
	}
	items := []queueFinding{}
	caseSet := map[string]bool{}
	for rows.Next() {
		var item queueFinding
		var finding, assessment, followup []byte
		if err = rows.Scan(&finding, &item.item.CaseTitle, &item.item.CaseStatus, &assessment, &followup); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal(finding, &item.item.Finding); err != nil {
			rows.Close()
			return result, err
		}
		f := &item.item.Finding
		f.CreatedAt = f.CreatedAt.UTC()
		f.UpdatedAt = f.UpdatedAt.UTC()
		item.assessment = workspace.Assessment{CaseID: f.CaseID, FindingID: f.ID}
		if len(assessment) > 0 {
			if err = json.Unmarshal(assessment, &item.assessment); err != nil {
				rows.Close()
				return result, err
			}
		}
		item.item.Followup = workspace.Followup{CaseID: f.CaseID, FindingID: f.ID}
		if len(followup) > 0 && string(followup) != "null" {
			if err = json.Unmarshal(followup, &item.item.Followup); err != nil {
				rows.Close()
				return result, err
			}
			item.item.Followup.Recorded = true
		}
		if item.item.Followup.DueAt != nil {
			at := item.item.Followup.DueAt.UTC()
			item.item.Followup.DueAt = &at
		}
		if item.item.Followup.UpdatedAt != nil {
			at := item.item.Followup.UpdatedAt.UTC()
			item.item.Followup.UpdatedAt = &at
		}
		items = append(items, item)
		caseSet[f.CaseID] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	caseIDs := make([]string, 0, len(caseSet))
	for id := range caseSet {
		caseIDs = append(caseIDs, id)
	}
	sort.Strings(caseIDs)
	basis, err := loadQueueBasisRows(ctx, tx, caseIDs)
	if err != nil {
		return result, err
	}
	all := make([]workspace.QueueItem, 0, len(items))
	for i, item := range items {
		if i%128 == 0 && ctx.Err() != nil {
			return result, ctx.Err()
		}
		a := item.assessment
		row := item.item
		var oldReview, oldProgress *workspace.ReviewBasis
		row.Conclusion = "unrecorded"
		if a.Review != nil {
			row.Conclusion = a.Review.Conclusion
			oldReview = &a.Review.Basis
		}
		if a.Progress != nil {
			oldProgress = &a.Progress.Basis
		}
		review, e := basis.evaluate(row.Finding, oldReview)
		if e != nil {
			return result, e
		}
		progress, e := basis.evaluate(row.Finding, oldProgress)
		if e != nil {
			return result, e
		}
		seenUpdates := map[string]bool{}
		for _, u := range append(review.ResourceUpdates, progress.ResourceUpdates...) {
			key := workspace.ResourceChoiceKey(u.Reference)
			if !seenUpdates[key] {
				row.ResourceUpdates = append(row.ResourceUpdates, u)
				seenUpdates[key] = true
			}
		}
		row.ReviewBasis, row.ProgressBasis = review.State, progress.State
		row.Basis = workspace.FindingBasisState(review, progress)
		row.BasisRevision = assessmentRequestHash([]workspace.BasisEvaluation{review, progress})
		row.AssessmentVersion = a.Version
		row.Progress = workspace.FindingProgressState(a)
		row.Overdue = workspace.FindingIsOverdue(a, row.Followup, result.EvaluatedAt)
		row.AssigneeState = "unassigned"
		if row.Followup.AssigneeSubjectID != nil {
			row.AssigneeState = access.AssigneeStates[*row.Followup.AssigneeSubjectID]
			if row.AssigneeState == "" {
				row.AssigneeState = "not_found"
			}
		}
		all = append(all, row)
	}
	from, to := filters.Window(result.EvaluatedAt)
	selected := []workspace.QueueItem{}
	for _, row := range all {
		if !queueMatches(row, filters, access.Subject) {
			continue
		}
		current := row.Progress != "ended"
		created := !row.Finding.CreatedAt.Before(from) && row.Finding.CreatedAt.Before(to)
		changed := row.Basis == "changed" || row.Basis == "unavailable"
		if current {
			result.Statistics.Current++
			if row.Followup.AssigneeSubjectID == nil {
				result.Statistics.Unassigned++
			}
		}
		if created {
			result.Statistics.Created++
		}
		if changed {
			result.Statistics.Reassessment++
		}
		if row.Overdue {
			result.Statistics.Overdue++
		}
		switch row.Basis {
		case "unrecorded":
			result.Statistics.BasisUnrecorded++
		case "changed":
			result.Statistics.BasisChanged++
		case "unavailable":
			result.Statistics.BasisUnavailable++
		}
		if (filters.Mode == "current" && current) || (filters.Mode == "created" && created) || (filters.Mode == "reassessment" && changed) {
			selected = append(selected, row)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		a, b := selected[i], selected[j]
		if filters.Sort == "due_asc" {
			if (a.Followup.DueAt == nil) != (b.Followup.DueAt == nil) {
				return a.Followup.DueAt != nil
			}
			if a.Followup.DueAt != nil && !a.Followup.DueAt.Equal(*b.Followup.DueAt) {
				return a.Followup.DueAt.Before(*b.Followup.DueAt)
			}
		}
		if !a.Finding.CreatedAt.Equal(b.Finding.CreatedAt) {
			return a.Finding.CreatedAt.After(b.Finding.CreatedAt)
		}
		return a.Finding.ID < b.Finding.ID
	})
	result.Total = len(selected)
	// Bind the fixed evaluation time and the complete observable filtered result.
	// Excluded rows still participate in matching/statistics above, but edits that
	// change neither the returned collection nor its statistics do not break it.
	result.Version = assessmentRequestHash(struct {
		At         time.Time
		Statistics workspace.QueueStatistics
		Items      []workspace.QueueItem
	}{result.EvaluatedAt, result.Statistics, selected})
	if request.Cursor != "" && cursor.Version != result.Version {
		return result, workspace.ErrConflict
	}
	start := 0
	if request.Cursor != "" {
		found := false
		for i, row := range selected {
			if row.Finding.ID == cursor.After {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return result, workspace.ErrInvalid
		}
	}
	end := start + request.Limit
	if end > len(selected) {
		end = len(selected)
	}
	result.Items = append(result.Items, selected[start:end]...)
	if end < len(selected) {
		cursor.After = selected[end-1].Finding.ID
		cursor.Version = result.Version
		raw, _ := json.Marshal(cursor)
		result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, commitWorkspaceTx(ctx, tx)
}

func queueMatches(row workspace.QueueItem, f workspace.QueueFilters, subject string) bool {
	assignee := f.Assignee
	if assignee == "me" {
		assignee = subject
	}
	switch assignee {
	case "":
	case "unassigned":
		if row.Followup.AssigneeSubjectID != nil {
			return false
		}
	case "inactive":
		if row.Followup.AssigneeSubjectID == nil || row.AssigneeState == "active" {
			return false
		}
	default:
		if row.Followup.AssigneeSubjectID == nil || *row.Followup.AssigneeSubjectID != assignee {
			return false
		}
	}
	if f.Progress != "" && f.Progress != row.Progress {
		return false
	}
	if f.Conclusion != "" && f.Conclusion != row.Conclusion {
		return false
	}
	if f.Basis != "" && f.Basis != row.Basis {
		return false
	}
	if f.CaseStatus != "" && f.CaseStatus != row.CaseStatus {
		return false
	}
	return f.Search == "" || strings.Contains(strings.ToLower(row.Finding.Title+"\n"+row.Finding.Statement+"\n"+row.CaseTitle), strings.ToLower(f.Search))
}

func loadQueueBasisRows(ctx context.Context, q workspaceQueryer, caseIDs []string) (queueBasisRows, error) {
	b := queueBasisRows{contents: map[string]workspace.ContentChoice{}, resources: map[string]workspace.AdoptedResource{}, known: map[string]workspace.BasisSelection{}, evidence: map[string]queueEvidence{}, gaps: map[string]queueGap{}}
	if len(caseIDs) == 0 {
		return b, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT l.case_id,COALESCE(l.finding_id,''),l.evidence_event_id::text,l.relation,l.event_hash_snapshot,l.source_ref_snapshot,COALESCE(e.event_hash,''),COALESCE(e.source_ref,'') FROM case_evidence_links l LEFT JOIN evidence_events e ON e.id=l.evidence_event_id WHERE l.case_id=ANY($1::text[]) ORDER BY l.case_id,l.finding_id,l.evidence_event_id,l.relation`, caseIDs)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var c, f string
		var e queueEvidence
		if err = rows.Scan(&c, &f, &e.link.EvidenceEventID, &e.link.Relation, &e.link.EventHash, &e.link.SourceRef, &e.hash, &e.ref); err != nil {
			rows.Close()
			return b, err
		}
		e.link.Scope = "finding"
		if f == "" {
			e.link.Scope = "case"
		}
		b.evidence[queueLinkKey(c, f, workspace.EvidenceChoiceKey(e.link))] = e
		if f != "" {
			known := b.known[f]
			known.Evidence = append(known.Evidence, e.link)
			b.known[f] = known
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	rows, err = q.QueryContext(ctx, `SELECT l.case_id,COALESCE(l.finding_id,''),l.gap_id::text,l.relation,l.affected_claim_snapshot,l.gap_type_snapshot,COALESCE(g.affected_claim,''),COALESCE(g.gap_type,''),r.version,r.state,r.note,r.updated_by_subject,r.updated_at FROM case_gap_links l LEFT JOIN capture_gaps g ON g.id=l.gap_id LEFT JOIN capture_gap_resolutions r ON r.gap_id=g.id WHERE l.case_id=ANY($1::text[]) ORDER BY l.case_id,l.finding_id,l.gap_id,l.relation`, caseIDs)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var c, f string
		var g queueGap
		var version sql.NullInt64
		var state, note, subject sql.NullString
		var at sql.NullTime
		if err = rows.Scan(&c, &f, &g.link.GapID, &g.link.Relation, &g.link.AffectedClaim, &g.link.GapType, &g.row.claim, &g.row.kind, &version, &state, &note, &subject, &at); err != nil {
			rows.Close()
			return b, err
		}
		g.link.Scope = "finding"
		if f == "" {
			g.link.Scope = "case"
		}
		if version.Valid {
			if !state.Valid || !note.Valid || !subject.Valid || !at.Valid {
				rows.Close()
				return b, workspace.ErrConflict
			}
			t := at.Time.UTC()
			g.row.adopted = workspace.AdoptedGap{ResolutionRecorded: true, ResolutionVersion: version.Int64, State: state.String, Note: note.String, UpdatedBySubject: subject.String, UpdatedAt: &t}
		}
		b.gaps[queueLinkKey(c, f, workspace.GapChoiceKey(g.link))] = g
		if f != "" {
			known := b.known[f]
			known.Gaps = append(known.Gaps, g.link)
			b.known[f] = known
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	resourceRows, e := q.QueryContext(ctx, `SELECT l.case_id,COALESCE(l.finding_id,''),l.reference_snapshot,d.record_bytes FROM case_resource_links l JOIN resource_access_records d ON `+resourceLinkJoin+` WHERE l.case_id=ANY($1::text[])`, caseIDs)
	if e != nil {
		return b, e
	}
	for resourceRows.Next() {
		var c, f string
		var ref, raw []byte
		if e = resourceRows.Scan(&c, &f, &ref, &raw); e != nil {
			resourceRows.Close()
			return b, e
		}
		var adopted workspace.AdoptedResource
		if decodeAssessmentRecord(ref, &adopted.Reference) != nil || decodeAssessmentRecord(raw, &adopted.Record) != nil || !workspace.ResourceMatchesRecord(adopted.Reference, adopted.Record) {
			resourceRows.Close()
			return b, workspace.ErrConflict
		}
		b.resources[queueLinkKey(c, f, workspace.ResourceChoiceKey(adopted.Reference))] = adopted
		if f != "" {
			known := b.known[f]
			known.Resources = append(known.Resources, adopted.Reference)
			b.known[f] = known
		}
	}
	e = resourceRows.Err()
	resourceRows.Close()
	if e != nil {
		return b, e
	}
	contentLinks, err := loadContentLinks(ctx, q, caseIDs)
	if err != nil {
		return b, err
	}
	for _, link := range contentLinks {
		if _, err = contentRequirements(ctx, q, link.Reference); err != nil {
			return b, err
		}
		b.contents[queueLinkKey(link.CaseID, link.FindingID, workspace.ContentChoiceKey(link.Reference))] = link.Reference
		if link.FindingID != "" {
			known := b.known[link.FindingID]
			known.Contents = append(known.Contents, link.Reference)
			b.known[link.FindingID] = known
		}
	}
	refs := make([]workspace.ResourceChoice, 0, len(b.resources))
	for _, r := range b.resources {
		refs = append(refs, r.Reference)
	}
	b.resourceUpdates, err = loadResourceUpdates(ctx, q, refs)
	if err != nil {
		return b, err
	}
	for id, known := range b.known {
		value, e := workspace.NormalizeBasisSelection(known, workspace.MaxKnownReferences)
		if e != nil {
			return b, e
		}
		b.known[id] = value
	}
	return b, nil
}
func (b queueBasisRows) evaluate(f workspace.Finding, old *workspace.ReviewBasis) (workspace.BasisEvaluation, error) {
	if old == nil {
		return workspace.EvaluateReviewBasis(nil, workspace.ReviewBasis{}, nil)
	}
	evidenceLookup := func(e workspace.EvidenceChoice) (workspace.EvidenceChoice, string, string, error) {
		id := f.ID
		if e.Scope == "case" {
			id = ""
		}
		v, ok := b.evidence[queueLinkKey(f.CaseID, id, workspace.EvidenceChoiceKey(e))]
		if !ok {
			return e, "", "", workspace.ErrNotFound
		}
		return v.link, v.hash, v.ref, nil
	}
	gapLookup := func(g workspace.GapChoice) (workspace.GapChoice, selectedGapRow, error) {
		id := f.ID
		if g.Scope == "case" {
			id = ""
		}
		v, ok := b.gaps[queueLinkKey(f.CaseID, id, workspace.GapChoiceKey(g))]
		if !ok {
			return g, selectedGapRow{}, workspace.ErrNotFound
		}
		return v.link, v.row, nil
	}
	resourceLookup := func(r workspace.ResourceChoice) (workspace.AdoptedResource, error) {
		id := f.ID
		if r.Scope == "case" {
			id = ""
		}
		v, ok := b.resources[queueLinkKey(f.CaseID, id, workspace.ResourceChoiceKey(r))]
		if !ok || v.Reference != r {
			return v, workspace.ErrNotFound
		}
		return v, nil
	}
	contentLookup := func(c workspace.ContentChoice) error {
		id := f.ID
		if c.Scope == "case" {
			id = ""
		}
		stored, ok := b.contents[queueLinkKey(f.CaseID, id, workspace.ContentChoiceKey(c))]
		if !ok || workspace.ContentChoiceKey(stored) != workspace.ContentChoiceKey(c) {
			return workspace.ErrNotFound
		}
		return nil
	}
	current, err := assembleReviewBasisWithContent(f.Statement, f.Version, old.Selected, b.known[f.ID], evidenceLookup, gapLookup, contentLookup, resourceLookup)
	v, err := workspace.EvaluateReviewBasis(old, current, err)
	return workspace.ApplyResourceUpdates(v, b.resourceUpdates, old.Selected.Resources), err
}
