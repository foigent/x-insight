package connectoractivation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"slices"
	"strconv"
	"time"
)

type ManagedProjects struct {
	Store            *db.WorkspaceStore
	Sources          *db.SourceFactsStore
	Host             *hostctl.Engine
	EnrollmentClient func(context.Context, connectorcontrol.ManagedBoundary) (*urmachine.EnrollmentClient, error)
	PolicyClient     func(context.Context, connectorcontrol.ManagedBoundary) (*urmachine.Client, error)
	TLSRoots         *x509.CertPool
	Now              func() time.Time
}

func (p *ManagedProjects) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}
func (p *ManagedProjects) Refresh(ctx context.Context) error {
	if p == nil || p.Store == nil || p.Sources == nil || p.Host == nil || p.EnrollmentClient == nil {
		return connectorcontrol.ErrInvalid
	}
	ids, e := p.Store.ListManagedConnectorIDs(ctx, 100)
	if e != nil {
		return e
	}
	var failures []error
	for _, id := range ids {
		ctx, finish, gateErr := connectorsync.Work(ctx, p.Store, id, "projects")
		if gateErr != nil {
			if !connectorsync.IsControlStop(gateErr) {
				failures = append(failures, gateErr)
			}
			continue
		}
		// Finish before moving to the next connection, including early continues.
		func() {
			var connectionErr error
			changes := map[string]sourcefacts.ManagedConfig{}
			applied := []db.ManagedEnrollment{}
			defer func() { finish(connectionErr) }()
			b, e := p.Store.InspectManagedConnector(ctx, id)
			if e != nil {
				connectionErr = e
				failures = append(failures, e)
				return
			}
			verified, qualified := b.VerifiedSource()
			if !qualified || b.Installation.Status != "connected" {
				connectorsync.Progress(ctx, true, 0)
				return
			}
			if e = p.qualifyRoot(ctx, b); e != nil {
				connectionErr = e
				failures = append(failures, e)
				return
			}
			// Both public deliveries remain independent; a successful purpose-specific
			// read is the first evidence of its usable delegated grant.
			if phase := b.Phase(connectorcontrol.PhaseDeliverMachineCandidate, connectorcontrol.PolicyMachinePurpose); phase != nil && phase.Operation.Status == "completed" && p.PolicyClient != nil {
				client, e := p.PolicyClient(ctx, b)
				if e == nil {
					_, e = client.PolicyScopes(ctx, "")
				}
				if e != nil && !errors.Is(e, urmachine.ErrUnauthorized) {
					connectionErr = e
					failures = append(failures, e)
				}
			}
			phase := b.Phase(connectorcontrol.PhaseDeliverMachineCandidate, connectorcontrol.EnrollmentMachinePurpose)
			if phase == nil || phase.Operation.Status != "completed" {
				connectorsync.Progress(ctx, true, 0)
				return
			}
			client, e := p.EnrollmentClient(ctx, b)
			if e != nil {
				connectionErr = e
				failures = append(failures, e)
				return
			}
			projects, e := allEnrollmentProjects(ctx, client)
			if errors.Is(e, urmachine.ErrUnauthorized) {
				return
			}
			if e != nil {
				connectionErr = e
				failures = append(failures, e)
				return
			}
			b, e = p.Store.InspectManagedConnector(ctx, id)
			if e == nil {
				e = p.qualifyRoot(ctx, b)
			}
			if e != nil {
				connectionErr = e
				failures = append(failures, e)
				return
			}
			for _, project := range projects {
				for _, enrollment := range project.Enrollments {
					record := db.ManagedEnrollment{InstallationID: id, EnrollmentID: enrollment.EnrollmentID, SourceInstance: verified.SourceInstance, OrgID: strconv.FormatInt(verified.OrgID, 10), ProjectID: project.ProjectID, Revision: enrollment.Revision, Status: "pending"}
					if enrollment.FactsStatus == "requires_migration" || enrollment.ContentStatus == "requires_migration" {
						record.Status = "requires_migration"
						if e = p.Store.SaveManagedEnrollment(ctx, record, p.now()); e != nil {
							connectionErr = e
							failures = append(failures, e)
						}
						continue
					}
					old, e := p.Store.ManagedEnrollment(ctx, id, enrollment.EnrollmentID, project.ProjectID)
					if e != nil && !errors.Is(e, connectorcontrol.ErrNotFound) {
						connectionErr = e
						failures = append(failures, e)
						continue
					}
					if enrollment.FactsStatus == "revoked" || enrollment.ContentStatus == "revoked" {
						if old.Status != "" {
							if e = p.revoke(ctx, b, project.ProjectID, enrollment); e != nil {
								connectionErr = e
								failures = append(failures, e)
								continue
							}
						}
						factsUsable := enrollment.FactsStatus != "revoked" && enrollment.FactsStatus != "not_requested"
						contentUsable := enrollment.IncludeContent && enrollment.ContentStatus != "revoked" && enrollment.ContentStatus != "not_requested"
						if factsUsable || contentUsable {
							// Each purpose has independent revocation; the unaffected
							// original collection remains qualified below.
						} else {
							record.Status = "revoked"
							if e = p.Store.SaveManagedEnrollment(ctx, record, p.now()); e != nil {
								connectionErr = e
								failures = append(failures, e)
							}
							continue
						}
					}
					if old.Status == "revoked" {
						continue
					}
					cfg, original, e := p.qualifyOriginalProject(ctx, b, project.ProjectID, enrollment)
					if e != nil {
						e = fmt.Errorf("qualify original managed project: %w", e)
					}
					if e == nil && !original {
						if old.Status == "requires_migration" {
							e = sourcefacts.ErrScopeChanged
						} else {
							cfg, e = p.configure(ctx, b, client, project.ProjectID, enrollment)
							if e != nil {
								e = fmt.Errorf("configure new managed project: %w", e)
							}
						}
					}
					if e != nil {
						if errors.Is(e, sourcefacts.ErrConflict) || errors.Is(e, sourcefacts.ErrScopeChanged) || errors.Is(e, urmachine.ErrConflict) {
							record.Status = "requires_migration"
							if save := p.Store.SaveManagedEnrollment(ctx, record, p.now()); save != nil {
								failures = append(failures, save)
							}
							failures = append(failures, fmt.Errorf("managed enrollment requires migration after qualification: %w", e))
						} else {
							connectionErr = e
							failures = append(failures, e)
						}
						continue
					}
					raw, e := json.Marshal(cfg)
					if e != nil {
						connectionErr = e
						failures = append(failures, e)
						continue
					}
					record.ConfigurationSHA256 = sourcefacts.Hash(raw)
					if len(cfg.Contents) > 0 {
						record.ContentCollectionID = cfg.Contents[0].CollectionID
					}
					if old.Status == "active" && old.ConfigurationSHA256 == record.ConfigurationSHA256 && old.Revision == record.Revision {
						continue
					}
					if original {
						// Original worker and mounts were read back. It already runs
						// these exact streams; a second partition would duplicate them.
						record.Status = "active"
						// Original C keeps its existing source reads until every still
						// readable historical representation has durable XI custody.
						// Owner maintenance clears the marker after verifying copies.
						if record.ContentCollectionID != "" && (old.ContentCollectionID == "" || old.LegacyCCopyPending) {
							record.Status = "requires_migration"
							record.LegacyCCopyPending = true
						}
						if e = p.Store.SaveManagedEnrollment(ctx, record, p.now()); e != nil {
							connectionErr = e
							failures = append(failures, e)
						}
						continue
					}
					// Persist custody-only membership before any worker may commit copied C.
					pending := record
					pending.ConfigurationSHA256 = ""
					if e = p.Store.SaveManagedEnrollment(ctx, pending, p.now()); e != nil {
						connectionErr = e
						failures = append(failures, e)
						continue
					}
					changes[id+"-"+enrollment.EnrollmentID+"-project-"+project.ProjectID+".json"] = cfg
					record.Status = "active"
					applied = append(applied, record)
				}
			}
			if len(changes) > 0 {
				if e = p.Host.PublishManagedProjects(ctx, changes); e != nil {
					connectionErr = e
					failures = append(failures, e)
				} else {
					for _, record := range applied {
						if e = p.Store.SaveManagedEnrollment(ctx, record, p.now()); e != nil {
							connectionErr = e
							failures = append(failures, e)
						}
					}
				}
			}
		}()
	}
	return errors.Join(failures...)
}

func (p *ManagedProjects) qualifyRoot(ctx context.Context, b connectorcontrol.ManagedBoundary) error {
	if b.Installation.Status != "connected" || b.Installation.DesiredState != "active" {
		return connectorcontrol.ErrConflict
	}
	if b.Bootstrap == nil {
		r, w, e := connectorcontrol.OriginalReceiverProof(b)
		if e != nil {
			return e
		}
		return p.Host.WithExistingFeed(ctx, w, r, func() error { return nil })
	}
	bind := b.Phase(connectorcontrol.PhaseBindExistingReceiver, "")
	if bind == nil || bind.Operation.Status != "completed" || bind.Operation.RequestV3 == nil || bind.Operation.RequestV3.Candidate == nil {
		return connectorcontrol.ErrConflict
	}
	c := bind.Operation.RequestV3.Candidate
	return p.Host.WithExistingFeed(ctx, c.WorkerConfig, c.ReceiverRegistration, func() error { return nil })
}
func allEnrollmentProjects(ctx context.Context, client *urmachine.EnrollmentClient) ([]urmachine.EnrollmentProject, error) {
	cursor := ""
	seen := map[string]bool{}
	projects := []urmachine.EnrollmentProject{}
	enrollments := map[string]bool{}
	for {
		page, e := client.Projects(ctx, cursor)
		if e != nil {
			return nil, e
		}
		for _, project := range page.Projects {
			for _, enrollment := range project.Enrollments {
				if enrollments[project.ProjectID+"/"+enrollment.EnrollmentID] {
					return nil, urmachine.ErrContract
				}
				enrollments[project.ProjectID+"/"+enrollment.EnrollmentID] = true
			}
			projects = append(projects, project)
		}
		if page.NextCursor == "" {
			return projects, nil
		}
		if seen[page.NextCursor] || page.NextCursor == cursor {
			return nil, urmachine.ErrContract
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
}
func (p *ManagedProjects) sourceReader(endpoint, keyFile string) (*pullfeed.HTTPSource, error) {
	key, e := config.LoadOwnerOnlySecretFile(keyFile)
	if e != nil {
		return nil, e
	}
	return pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{SyncRequired: true, Endpoint: endpoint, ServiceKey: key, TLSRootCAs: p.TLSRoots, RequestTimeout: 5 * time.Second})
}
func (p *ManagedProjects) configure(ctx context.Context, b connectorcontrol.ManagedBoundary, client *urmachine.EnrollmentClient, project string, enrollment urmachine.Enrollment) (sourcefacts.ManagedConfig, error) {
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedObservationConfigVersion, PollIntervalSeconds: 30, RetryMaximumSeconds: 300, Profiles: []sourcefacts.ManagedProfileConfig{}}
	verified, ok := b.VerifiedSource()
	if !ok {
		return cfg, connectorcontrol.ErrConflict
	}
	source, org, id := verified.SourceInstance, strconv.FormatInt(verified.OrgID, 10), b.Installation.InstallationID
	var facts *hostctl.SourcePreparation
	var factsReceipt urmachine.RegistrationReceipt
	if enrollment.FactsStatus != "not_requested" && enrollment.FactsStatus != "revoked" {
		profiles := append([]string(nil), enrollment.Profiles...)
		if !slices.IsSorted(profiles) {
			return cfg, urmachine.ErrContract
		}
		prepared, e := p.Host.PrepareSourceCredential(ctx, id, enrollment.EnrollmentID, "facts", source, org, project, profiles)
		if e != nil {
			return cfg, fmt.Errorf("prepare managed facts custody: %w", e)
		}
		facts = &prepared
		raw, _ := json.Marshal(prepared.Registration)
		factsReceipt, e = client.Register(ctx, project, enrollment.EnrollmentID, "facts", raw)
		if e != nil {
			return cfg, fmt.Errorf("register managed facts source: %w", e)
		}
		if factsReceipt.RegistrationHash != prepared.RegistrationSHA256 {
			return cfg, urmachine.ErrContract
		}
		budgetProfiles := []string{}
		for _, profile := range profiles {
			if !sourcefacts.ValidProfile(profile) && !sourcefacts.ValidObservationProfile(profile) {
				budgetProfiles = append(budgetProfiles, profile)
				continue
			}
			endpoint := b.UniRouterOrigin + sourcefacts.ManagedEndpointPath(profile)
			reader, e := p.sourceReader(endpoint, prepared.KeyFile)
			if e != nil {
				return cfg, e
			}
			raw, e := reader.FetchSourceCapabilities(ctx)
			reader.CloseContentConnections()
			if e != nil {
				return cfg, e
			}
			var caps sourcefacts.Capabilities
			if sourcefacts.ValidProfile(profile) {
				caps, e = sourcefacts.DecodeCapabilities(raw)
			} else {
				caps, e = sourcefacts.DecodeObservationCapabilities(raw)
			}
			if e != nil || caps.SourceInstance != source || caps.Status != "active" || caps.CredentialStatus != "feed_active" || !slices.Contains(caps.Profiles, profile) {
				return cfg, fmt.Errorf("managed facts capability scope for %s: %w", profile, sourcefacts.ErrScopeChanged)
			}
			stream := sourcefacts.Stream{ID: hostctl.ManagedCollectionID(id, enrollment.EnrollmentID, project, "profile:"+profile), SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: source, OrgID: org, ProjectID: project, Profile: profile, ScopeGeneration: caps.ScopeGeneration}
			live, e := p.Sources.RegisterSourceFactsStream(ctx, stream)
			if e != nil {
				return cfg, fmt.Errorf("persist managed facts registration for %s: %w", profile, e)
			}
			if live.RequiresAttention || live.Status == "revoked" {
				return cfg, fmt.Errorf("managed facts registration state for %s: %w", profile, sourcefacts.ErrScopeChanged)
			}
			if live.Status != "active" {
				if live.Status == "revoked" {
					return cfg, sourcefacts.ErrScopeChanged
				}
				if _, e = p.Sources.ControlSourceFactsStream(ctx, live.ID, "active", live.Revision, false); e != nil {
					return cfg, fmt.Errorf("activate managed facts registration for %s: %w", profile, e)
				}
			}
			cfg.Profiles = append(cfg.Profiles, sourcefacts.ManagedProfileConfig{StreamID: stream.ID, SourceSystemID: stream.SourceSystemID, SourceInstance: source, OrgID: org, ProjectID: project, Profile: profile, ScopeGeneration: caps.ScopeGeneration, Endpoint: endpoint, KeyFile: prepared.KeyFile})
		}
		if len(budgetProfiles) > 0 {
			reader, e := p.sourceReader(b.UniRouterOrigin+sourcefacts.BudgetEndpointPath, prepared.KeyFile)
			if e != nil {
				return cfg, e
			}
			raw, e := reader.FetchSourceCapabilities(ctx)
			reader.CloseContentConnections()
			if e != nil {
				return cfg, e
			}
			caps, e := sourcefacts.DecodeObservationCapabilities(raw)
			if e != nil {
				return cfg, e
			}
			budget := sourcefacts.ManagedBudgetConfig{CollectionID: hostctl.ManagedCollectionID(id, enrollment.EnrollmentID, project, "budget"), SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: source, OrgID: org, ProjectID: project, Profiles: budgetProfiles, ScopeGeneration: caps.ScopeGeneration, Endpoint: b.UniRouterOrigin + sourcefacts.BudgetEndpointPath, KeyFile: prepared.KeyFile}
			reader, e = p.sourceReader(budget.Endpoint, prepared.KeyFile)
			if e != nil {
				return cfg, e
			}
			_, e = sourcefacts.PreflightBudget(ctx, reader, budget.Registration())
			reader.CloseContentConnections()
			if e != nil {
				return cfg, e
			}
			live, e := p.Sources.RegisterBudgetCollection(ctx, budget.Registration())
			if e != nil {
				return cfg, e
			}
			if live.Status != "active" {
				if live.RequiresAttention || live.Status == "revoked" {
					return cfg, fmt.Errorf("managed budget registration state: %w", sourcefacts.ErrScopeChanged)
				}
				if _, e = p.Sources.ControlBudgetCollection(ctx, live.ID, "active", live.Revision, false); e != nil {
					return cfg, e
				}
			}
			cfg.SchemaVersion = sourcefacts.ManagedBudgetConfigVersion
			cfg.Budgets = []sourcefacts.ManagedBudgetConfig{budget}
		}
	}
	if enrollment.IncludeContent && enrollment.ContentStatus != "not_requested" && enrollment.ContentStatus != "revoked" {
		prepared, e := p.Host.PrepareSourceCredential(ctx, id, enrollment.EnrollmentID, "content", source, org, project, []string{sourcefacts.ContentProfile})
		if e != nil {
			return cfg, e
		}
		raw, _, e := prepared.Registration.MarshalRegistration()
		if e != nil {
			return cfg, e
		}
		receipt, e := client.Register(ctx, project, enrollment.EnrollmentID, "content", raw)
		if e != nil {
			return cfg, fmt.Errorf("register managed content source: %w", e)
		}
		reader, e := p.sourceReader(b.UniRouterOrigin+sourcefacts.ContentEndpointPath, prepared.KeyFile)
		if e != nil {
			return cfg, e
		}
		raw, e = reader.FetchContentCapabilities(ctx)
		reader.CloseContentConnections()
		if e != nil {
			return cfg, e
		}
		caps, e := sourcefacts.DecodeContentCapabilities(raw)
		if e != nil || caps.SourceInstance != source || caps.OrgID != org || caps.ProjectID != project || caps.Status != "active" || caps.CredentialStatus != "feed_active" {
			return cfg, fmt.Errorf("managed content capability scope (decoded=%t source=%t org=%t project=%t active=%t credential_active=%t): %w", e == nil, caps.SourceInstance == source, caps.OrgID == org, caps.ProjectID == project, caps.Status == "active", caps.CredentialStatus == "feed_active", sourcefacts.ErrScopeChanged)
		}
		c := sourcefacts.ContentCollection{ID: hostctl.ManagedCollectionID(id, enrollment.EnrollmentID, project, "collection:content"), SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: source, OrgID: org, ProjectID: project, CredentialID: receipt.CredentialID, RegistrationHash: receipt.RegistrationHash, ContentScopeRevision: caps.ContentScopeRevision, RecoveryEpoch: caps.RecoveryEpoch, AllowedPurposes: caps.AllowedPurposes, CoverageFrom: caps.CoverageFrom}
		live, e := p.Sources.RegisterContentCollection(ctx, c)
		if e != nil {
			return cfg, fmt.Errorf("persist managed content collection: %w", e)
		}
		// Activation CAS requires the exact persisted revision. Registration
		// assigns it locally; the new source projection has no revision yet.
		c.Revision = live.Revision
		reader, e = p.sourceReader(b.UniRouterOrigin+sourcefacts.ContentEndpointPath, prepared.KeyFile)
		if e != nil {
			return cfg, e
		}
		_, e = sourcefacts.PreflightContent(ctx, reader, c)
		reader.CloseContentConnections()
		if e != nil {
			return cfg, fmt.Errorf("managed content preflight: %w", e)
		}
		if live.Status != "active" {
			if live.RequiresAttention || live.Status == "revoked" {
				return cfg, fmt.Errorf("managed content registration state: %w", sourcefacts.ErrScopeChanged)
			}
			if _, e = p.Sources.ControlContentCollection(ctx, live.ID, "active", live.Revision, false, c); e != nil {
				return cfg, fmt.Errorf("activate managed content collection: %w", e)
			}
		}
		cfg.SchemaVersion = sourcefacts.ManagedContentConfigVersion
		if facts != nil {
			cfg.Bindings = append(cfg.Bindings, sourcefacts.ManagedCredentialBinding{Name: "facts", Scope: "legacy", SourceSystemID: c.SourceSystemID, SourceInstance: source, OrgID: org, ProjectID: project, CredentialID: factsReceipt.CredentialID, RegistrationHash: factsReceipt.RegistrationHash, KeyFile: facts.KeyFile})
		}
		cfg.Bindings = append(cfg.Bindings, sourcefacts.ManagedCredentialBinding{Name: "content", Scope: "content", SourceSystemID: c.SourceSystemID, SourceInstance: source, OrgID: org, ProjectID: project, CredentialID: c.CredentialID, RegistrationHash: c.RegistrationHash, KeyFile: prepared.KeyFile})
		cfg.Contents = []sourcefacts.ManagedContentConfig{{CollectionID: c.ID, SourceSystemID: c.SourceSystemID, SourceInstance: source, OrgID: org, ProjectID: project, CredentialBinding: "content", ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, AllowedPurposes: c.AllowedPurposes, CoverageFrom: c.CoverageFrom, Endpoint: b.UniRouterOrigin + sourcefacts.ContentEndpointPath}}
	}
	if e := cfg.Validate(); e != nil {
		return cfg, fmt.Errorf("managed project qualification: %w", e)
	}
	if e := p.ensureManagedContentProject(ctx, b, cfg); e != nil {
		return cfg, fmt.Errorf("bind managed content project: %w", e)
	}
	return cfg, nil
}

func (p *ManagedProjects) ensureManagedContentProject(ctx context.Context, b connectorcontrol.ManagedBoundary, cfg sourcefacts.ManagedConfig) error {
	if len(cfg.Contents) == 0 {
		return nil
	}
	c := cfg.Contents[0]
	_, err := p.Sources.EnsureManagedReviewCaptureProject(ctx, b.Installation.InstallationID,
		c.CollectionID, c.SourceSystemID, c.SourceInstance, c.OrgID, c.ProjectID, p.now())
	return err
}

// Revocation uses original configured identities after bind-existing. The
// generated identity is only the fallback for a newly managed project.
func managedRevocationTargets(base sourcefacts.ManagedConfig, b connectorcontrol.ManagedBoundary, project string, enrollment urmachine.Enrollment) (profiles []string, budgets []string, contents []string) {
	verified, ok := b.VerifiedSource()
	if !ok {
		return
	}
	id, source, org := b.Installation.InstallationID, verified.SourceInstance, strconv.FormatInt(verified.OrgID, 10)
	matches := func(instance, organization, candidateProject string) bool {
		return instance == source && organization == org && candidateProject == project
	}
	original := false
	for _, v := range base.Profiles {
		if matches(v.SourceInstance, v.OrgID, v.ProjectID) {
			original = true
			if enrollment.FactsStatus == "revoked" {
				profiles = append(profiles, v.StreamID)
			}
		}
	}
	for _, v := range base.Budgets {
		if matches(v.SourceInstance, v.OrgID, v.ProjectID) {
			original = true
			if enrollment.FactsStatus == "revoked" {
				budgets = append(budgets, v.CollectionID)
			}
		}
	}
	for _, v := range base.Contents {
		if matches(v.SourceInstance, v.OrgID, v.ProjectID) {
			original = true
			if enrollment.ContentStatus == "revoked" {
				contents = append(contents, v.CollectionID)
			}
		}
	}
	if original {
		return
	}
	if enrollment.FactsStatus == "revoked" {
		budget := false
		for _, profile := range enrollment.Profiles {
			if sourcefacts.ValidProfile(profile) || sourcefacts.ValidObservationProfile(profile) {
				profiles = append(profiles, hostctl.ManagedCollectionID(id, enrollment.EnrollmentID, project, "profile:"+profile))
			} else {
				budget = budget || sourcefacts.ValidBudgetProfiles([]string{profile})
			}
		}
		if budget {
			budgets = append(budgets, hostctl.ManagedCollectionID(id, enrollment.EnrollmentID, project, "budget"))
		}
	}
	if enrollment.ContentStatus == "revoked" {
		contents = append(contents, hostctl.ManagedCollectionID(id, enrollment.EnrollmentID, project, "collection:content"))
	}
	return
}
func (p *ManagedProjects) revoke(ctx context.Context, b connectorcontrol.ManagedBoundary, project string, enrollment urmachine.Enrollment) error {
	base, _, err := p.Host.ReadOriginalSourceConfig(ctx)
	if err != nil {
		return err
	}
	profiles, budgets, contents := managedRevocationTargets(base, b, project, enrollment)
	verified, ok := b.VerifiedSource()
	if !ok {
		return sourcefacts.ErrScopeChanged
	}
	source, org := verified.SourceInstance, strconv.FormatInt(verified.OrgID, 10)
	matches := func(instance, organization, candidateProject string) bool {
		return instance == source && organization == org && candidateProject == project
	}
	// Qualify every target before performing any revocation.
	streams := []sourcefacts.Stream{}
	collections := []sourcefacts.BudgetCollection{}
	bodies := []sourcefacts.ContentCollection{}
	for _, id := range profiles {
		live, err := p.Sources.LoadSourceFactsStream(ctx, id)
		if err != nil {
			return err
		}
		if !matches(live.SourceInstance, live.OrgID, live.ProjectID) || !slices.Contains(enrollment.Profiles, live.Profile) {
			return sourcefacts.ErrScopeChanged
		}
		streams = append(streams, live)
	}
	for _, id := range budgets {
		live, err := p.Sources.LoadBudgetCollection(ctx, id)
		if err != nil {
			return err
		}
		if !matches(live.SourceInstance, live.OrgID, live.ProjectID) {
			return sourcefacts.ErrScopeChanged
		}
		for _, profile := range live.Profiles {
			if !slices.Contains(enrollment.Profiles, profile) {
				return sourcefacts.ErrScopeChanged
			}
		}
		collections = append(collections, live)
	}
	for _, id := range contents {
		live, err := p.Sources.LoadContentCollection(ctx, id)
		if err != nil {
			return err
		}
		if !matches(live.SourceInstance, live.OrgID, live.ProjectID) {
			return sourcefacts.ErrScopeChanged
		}
		bodies = append(bodies, live)
	}
	for _, live := range streams {
		if live.Status != "revoked" {
			if _, err := p.Sources.ControlSourceFactsStream(ctx, live.ID, "revoked", live.Revision, false); err != nil {
				return err
			}
		}
	}
	for _, live := range collections {
		if live.Status != "revoked" {
			if _, err := p.Sources.ControlBudgetCollection(ctx, live.ID, "revoked", live.Revision, false); err != nil {
				return err
			}
		}
	}
	for _, live := range bodies {
		if live.Status != "revoked" {
			if _, err := p.Sources.ControlContentCollection(ctx, live.ID, "revoked", live.Revision, false); err != nil {
				return err
			}
		}
	}
	return nil
}
