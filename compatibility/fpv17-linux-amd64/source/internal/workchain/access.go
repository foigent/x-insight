// Package workchain carries the authenticated, request-local view of the
// existing workbench grants. It is not an additional authorization registry.
package workchain

import (
	"context"
	"errors"

	"github.com/foigent/x-insight/internal/workbench"
)

const ProjectionVersion = "xinsight.authorized-investigation.v1"
const LabelVersion = "xinsight.case-scope.v1"

var ErrDenied = errors.New("scoped resource is unavailable")
var ErrConfiguration = errors.New("scoped workchain requires distinct reader and workspace pools")

type Fields struct {
	Content     bool `json:"content,omitempty"`
	Resource    bool `json:"resource,omitempty"`
	Observation bool `json:"observation,omitempty"`
	Context     bool `json:"context"`
	Identity    bool `json:"identity"`
	Cost        bool `json:"cost"`
}

type ObservationFields struct {
	Runtime     bool `json:"runtime"`
	Attempt     bool `json:"attempt"`
	Capability  bool `json:"capability"`
	Policy      bool `json:"policy"`
	Destination bool `json:"destination"`
}

// A cost-project grant is not a wildcard for requests with no economic fact.
// Callers pass the actually matched cost project, never an inferred value.
func (a *Access) ObservationFieldsFor(source, project, costProject string) ObservationFields {
	var f ObservationFields
	if a == nil {
		return f
	}
	for _, g := range a.Authorization.Grants {
		if g.SourceSystemID != source || g.SourceProject != "" && g.SourceProject != project || g.CostProject != "" && g.CostProject != costProject {
			continue
		}
		f.Runtime = f.Runtime || g.AllowSourceRuntime
		f.Attempt = f.Attempt || g.AllowSourceAttempt
		f.Capability = f.Capability || g.AllowSourceCapability
		f.Policy = f.Policy || g.AllowSourcePolicy
		f.Destination = f.Destination || g.AllowSourceDestination
	}
	return f
}

func (f ObservationFields) Any() bool {
	return f.Runtime || f.Attempt || f.Capability || f.Policy || f.Destination
}

func (f Fields) Complete() bool { return f.Context && f.Identity && f.Cost }

type Requirement struct {
	Source      string `json:"source"`
	Project     string `json:"project"`
	CostProject string `json:"cost_project"`
	Fields      Fields `json:"fields"`
}

type Access struct {
	Subject         string
	Reviewer        bool
	PurposeReviewer bool
	Authorization   workbench.Authorization
	Revision        string
	CommitCheck     func(func() error) error
	Cases           map[any]map[string][]Requirement
}

// FieldsFor combines capabilities only after the whole grant scope matches.
// Empty data project values do not match project-specific grants.
func (a *Access) FieldsFor(source, project, costProject string) (Fields, bool) {
	var f Fields
	allowed := false
	for _, g := range a.Authorization.Grants {
		if !g.AllowDetails || g.SourceSystemID != source ||
			(g.SourceProject != "" && g.SourceProject != project) ||
			(g.CostProject != "" && g.CostProject != costProject) {
			continue
		}
		allowed = true
		f.Context = f.Context || g.AllowSourceContext
		f.Identity = f.Identity || g.AllowSourceIdentity
		f.Cost = f.Cost || g.AllowSourceCost
	}
	return f, allowed
}

// ContentAllowedFor requires explicit content/human_review in one matching grant.
// Empty costProject must be used when no economic association exists.
func (a *Access) ContentAllowedFor(source, project, costProject string) bool {
	if a == nil {
		return false
	}
	for _, g := range a.Authorization.Grants {
		if g.AllowContent && len(g.AllowedContentPurposes) == 1 && g.AllowedContentPurposes[0] == "human_review" && g.SourceSystemID == source && (g.SourceProject == "" || g.SourceProject == project) && (g.CostProject == "" || g.CostProject == costProject) {
			return true
		}
	}
	return false
}

func (a *Access) Allows(r Requirement) bool {
	if a == nil {
		return false
	}
	if r.Fields.Content {
		if !a.ContentAllowedFor(r.Source, r.Project, r.CostProject) {
			return false
		}
		if !r.Fields.Resource && !r.Fields.Observation && !r.Fields.Context && !r.Fields.Identity && !r.Fields.Cost {
			return true
		}
	}
	f, ok := a.FieldsFor(r.Source, r.Project, r.CostProject)
	if r.Fields.Resource || r.Fields.Observation {
		for _, g := range a.Authorization.Grants {
			if g.SourceSystemID == r.Source && (g.SourceProject == "" || g.SourceProject == r.Project) && (g.CostProject == "" || g.CostProject == r.CostProject) && ((!r.Fields.Resource || g.AllowResource) && (!r.Fields.Observation || g.AllowSourceRuntime || g.AllowSourceAttempt || g.AllowSourceCapability || g.AllowSourcePolicy || g.AllowSourceDestination)) {
				return true
			}
		}
		return false
	}
	return ok && (!r.Fields.Context || f.Context) && (!r.Fields.Identity || f.Identity) && (!r.Fields.Cost || f.Cost)
}

type contextKey struct{}

func WithAccess(ctx context.Context, a *Access) context.Context {
	return context.WithValue(ctx, contextKey{}, a)
}

func FromContext(ctx context.Context) *Access {
	a, _ := ctx.Value(contextKey{}).(*Access)
	return a
}
