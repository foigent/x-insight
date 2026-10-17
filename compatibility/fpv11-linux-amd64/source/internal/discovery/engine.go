package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/investigation"
)

const MethodVersion = "xinsight.discovery.deterministic.v1"

const maxHypothesisStatementBytes = 8000

type Observation struct {
	ExecutionID      string
	EvidenceEventIDs []string
	CoverageMissing  []string
	Sequence         []string
	ActorRef         string
	Provider         string
	PolicyRef        string
	LatencyMS        *int64
}

type Config struct {
	MinimumSample int
	GeneratedAt   time.Time
	RunID         string
	AnalysisAsOf  time.Time
	SampleSize    int
	InputDigest   string
}

func Analyze(observations []Observation, config Config) []investigation.DiscoveryHypothesis {
	minimum := config.MinimumSample
	if minimum < 20 {
		minimum = 100
	}
	generatedAt := config.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Unix(0, 0).UTC()
	}
	normalized := append([]Observation(nil), observations...)
	sort.Slice(normalized, func(left, right int) bool {
		return normalized[left].ExecutionID < normalized[right].ExecutionID
	})
	builders := []func([]Observation, int) hypothesisDraft{
		coverageDraft,
		processDraft,
		topicDraft,
		anomalyDraft,
		coalitionDraft,
		authorityDriftDraft,
	}
	items := make([]investigation.DiscoveryHypothesis, 0, len(builders))
	for _, builder := range builders {
		draft := builder(normalized, minimum)
		items = append(items, finalize(draft, generatedAt, config))
	}
	return items
}

type hypothesisDraft struct {
	kind             string
	title            string
	statement        string
	supportScore     string
	status           string
	evidenceIDs      []string
	executionIDs     []string
	limitations      []string
	abstentionReason string
}

func coverageDraft(items []Observation, minimum int) hypothesisDraft {
	base := baseDraft("coverage_pattern", "Coverage pattern")
	if len(items) < minimum {
		return abstain(base, len(items), minimum)
	}
	affected := make([]Observation, 0)
	for _, item := range items {
		if len(item.CoverageMissing) > 0 {
			affected = append(affected, item)
		}
	}
	ratio := float64(len(affected)) / float64(len(items))
	base.status = "candidate"
	base.supportScore = boundedSupportScore(len(items), minimum, ratio)
	base.statement = strconv.Itoa(len(affected)) + " of " + strconv.Itoa(len(items)) +
		" sampled executions contain at least one explicit incomplete coverage axis."
	base.executionIDs, base.evidenceIDs = refs(affected, 1000)
	base.limitations = []string{
		"Coverage reflects observed source and import contracts, not unobserved reality.",
		"Sampling and source mix may change the ratio.",
	}
	return base
}

func processDraft(items []Observation, minimum int) hypothesisDraft {
	base := baseDraft("process_pattern", "Repeated execution sequence")
	if len(items) < minimum {
		return abstain(base, len(items), minimum)
	}
	counts := map[string][]Observation{}
	for _, item := range items {
		if len(item.Sequence) == 0 {
			continue
		}
		key := strings.Join(item.Sequence, " → ")
		counts[key] = append(counts[key], item)
	}
	key, matches := mostCommon(counts)
	if len(matches) < 5 {
		base.abstentionReason = "No repeated explicit sequence met the five-execution support floor."
		base.statement = "The bounded sample does not support a repeated process sequence."
		return base
	}
	base.status = "candidate"
	base.supportScore = boundedSupportScore(len(matches), minimum/4, float64(len(matches))/float64(len(items)))
	base.statement = boundedStatement("Repeated explicit sequence: ", key)
	base.executionIDs, base.evidenceIDs = refs(matches, 1000)
	base.limitations = []string{
		"Only source-declared or directly observed sequence labels are included.",
		"Time adjacency is not converted into a causal edge.",
	}
	return base
}

func topicDraft(items []Observation, minimum int) hypothesisDraft {
	base := baseDraft("topic_cluster", "Metadata topic cluster")
	if len(items) < minimum {
		return abstain(base, len(items), minimum)
	}
	groups := map[string][]Observation{}
	for _, item := range items {
		if item.Provider != "" {
			groups["provider:"+item.Provider] = append(groups["provider:"+item.Provider], item)
		}
	}
	key, matches := mostCommon(groups)
	if len(matches) < 5 {
		base.abstentionReason = "No allowlisted metadata cluster met the five-execution support floor."
		base.statement = "The bounded metadata sample does not support a topic cluster."
		return base
	}
	base.status = "candidate"
	base.supportScore = boundedSupportScore(len(matches), minimum/4, float64(len(matches))/float64(len(items)))
	base.statement = boundedStatement("Allowlisted metadata cluster: ", key)
	base.executionIDs, base.evidenceIDs = refs(matches, 1000)
	base.limitations = []string{
		"No raw prompt or response content was used.",
		"Provider grouping is a metadata cohort, not semantic topic understanding.",
	}
	return base
}

func anomalyDraft(items []Observation, minimum int) hypothesisDraft {
	base := baseDraft("anomaly", "Latency anomaly candidate")
	values := make([]int64, 0, len(items))
	for _, item := range items {
		if item.LatencyMS != nil {
			values = append(values, *item.LatencyMS)
		}
	}
	if len(values) < minimum {
		return abstainWithObserved(base, len(values), minimum, "latency-bearing")
	}
	sort.Slice(values, func(left, right int) bool { return values[left] < values[right] })
	median := values[len(values)/2]
	deviations := make([]int64, len(values))
	for index, value := range values {
		if value >= median {
			deviations[index] = value - median
		} else {
			deviations[index] = median - value
		}
	}
	sort.Slice(deviations, func(left, right int) bool { return deviations[left] < deviations[right] })
	mad := deviations[len(deviations)/2]
	threshold := saturatedAnomalyThreshold(median, mad)
	matches := make([]Observation, 0)
	for _, item := range items {
		if item.LatencyMS != nil && *item.LatencyMS > threshold {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		base.abstentionReason = "No latency value exceeded the deterministic median plus six-MAD threshold."
		base.statement = "The bounded sample did not produce a latency anomaly candidate."
		return base
	}
	base.status = "candidate"
	base.supportScore = boundedSupportScore(len(values), minimum, 0.8)
	base.statement = "Observed latency exceeded deterministic threshold " + strconv.FormatInt(threshold, 10) + " ms."
	base.executionIDs, base.evidenceIDs = refs(matches, 1000)
	base.limitations = []string{
		"Latency cohorts are not normalized for workload or provider mix.",
		"Anomaly does not establish root cause.",
	}
	return base
}

func coalitionDraft(items []Observation, minimum int) hypothesisDraft {
	base := baseDraft("coalition", "Actor-provider coalition candidate")
	if len(items) < minimum {
		return abstain(base, len(items), minimum)
	}
	groups := map[string][]Observation{}
	for _, item := range items {
		if item.ActorRef != "" && item.Provider != "" {
			key := item.ActorRef + " → " + item.Provider
			groups[key] = append(groups[key], item)
		}
	}
	key, matches := mostCommon(groups)
	if len(matches) < 10 {
		base.abstentionReason = "No actor-provider pair met the ten-execution support floor."
		base.statement = "The bounded sample does not support a coalition candidate."
		return base
	}
	base.status = "candidate"
	base.supportScore = boundedSupportScore(len(matches), minimum/4, float64(len(matches))/float64(len(items)))
	base.statement = boundedStatement("Repeated actor-provider association: ", key)
	base.executionIDs, base.evidenceIDs = refs(matches, 1000)
	base.limitations = []string{
		"Association is not coordination, intent or abuse.",
		"Opaque actor references may represent shared technical identities.",
	}
	return base
}

func authorityDriftDraft(items []Observation, minimum int) hypothesisDraft {
	base := baseDraft("authority_drift", "Authority drift candidate")
	if len(items) < minimum {
		return abstain(base, len(items), minimum)
	}
	byActor := map[string]map[string][]Observation{}
	for _, item := range items {
		if item.ActorRef == "" || item.PolicyRef == "" {
			continue
		}
		if byActor[item.ActorRef] == nil {
			byActor[item.ActorRef] = map[string][]Observation{}
		}
		byActor[item.ActorRef][item.PolicyRef] = append(byActor[item.ActorRef][item.PolicyRef], item)
	}
	actors := make([]string, 0, len(byActor))
	for actor := range byActor {
		actors = append(actors, actor)
	}
	sort.Strings(actors)
	selectedActor := ""
	selectedPolicies := []string(nil)
	selectedMatches := []Observation(nil)
	for _, actor := range actors {
		policies := byActor[actor]
		if len(policies) < 2 {
			continue
		}
		matches := make([]Observation, 0)
		policyRefs := make([]string, 0, len(policies))
		for policy, observations := range policies {
			policyRefs = append(policyRefs, policy)
			matches = append(matches, observations...)
		}
		sort.Strings(policyRefs)
		if len(matches) > len(selectedMatches) {
			selectedActor = actor
			selectedPolicies = policyRefs
			selectedMatches = matches
		}
	}
	if selectedActor != "" {
		base.status = "candidate"
		base.supportScore = boundedSupportScore(len(selectedMatches), minimum/4, 0.7)
		base.statement = boundedStatement(
			"Actor "+selectedActor+" was observed under multiple explicit policy refs: ",
			strings.Join(selectedPolicies, ", "),
		)
		base.executionIDs, base.evidenceIDs = refs(selectedMatches, 1000)
		base.limitations = []string{
			"Multiple policy refs may be an intended migration.",
			"The hypothesis does not infer permission expansion without policy semantics.",
		}
		return base
	}
	base.abstentionReason = "No actor had two explicit policy references in the bounded sample."
	base.statement = "The bounded sample does not support an authority drift candidate."
	return base
}

func baseDraft(kind string, title string) hypothesisDraft {
	return hypothesisDraft{
		kind: kind, title: title, status: "abstained",
		limitations: []string{"The deterministic evidence threshold was not met."},
	}
}

func abstain(base hypothesisDraft, observed int, minimum int) hypothesisDraft {
	return abstainWithObserved(base, observed, minimum, "execution")
}

func abstainWithObserved(base hypothesisDraft, observed int, minimum int, label string) hypothesisDraft {
	base.statement = "Insufficient bounded evidence for " + base.kind + "."
	base.abstentionReason = "Observed " + strconv.Itoa(observed) + " " + label +
		" records; minimum is " + strconv.Itoa(minimum) + "."
	return base
}

func finalize(draft hypothesisDraft, generatedAt time.Time, config Config) investigation.DiscoveryHypothesis {
	identity := strings.Join([]string{
		MethodVersion, config.RunID, draft.kind, draft.title, draft.statement, draft.supportScore,
		draft.status, strings.Join(draft.evidenceIDs, ","), strings.Join(draft.executionIDs, ","),
		strings.Join(draft.limitations, ","), draft.abstentionReason,
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return investigation.DiscoveryHypothesis{
		ID:                  "xhy_" + hex.EncodeToString(digest[:]),
		AnalysisRunID:       config.RunID,
		AnalysisAsOf:        config.AnalysisAsOf.UTC(),
		SampleSize:          config.SampleSize,
		InputDigest:         config.InputDigest,
		HypothesisType:      draft.kind,
		Title:               draft.title,
		Statement:           draft.statement,
		SupportScoreDecimal: draft.supportScore,
		Status:              draft.status,
		EvidenceEventIDs:    nonNil(draft.evidenceIDs),
		ExecutionIDs:        nonNil(draft.executionIDs),
		Limitations:         nonNil(draft.limitations),
		AbstentionReason:    draft.abstentionReason,
		MethodVersion:       MethodVersion,
		GeneratedAt:         generatedAt,
	}
}

func refs(items []Observation, limit int) ([]string, []string) {
	executions := make([]string, 0)
	evidence := make([]string, 0)
	seenExecution := map[string]struct{}{}
	seenEvidence := map[string]struct{}{}
	for _, item := range items {
		if len(executions) < limit {
			if _, seen := seenExecution[item.ExecutionID]; !seen && item.ExecutionID != "" {
				seenExecution[item.ExecutionID] = struct{}{}
				executions = append(executions, item.ExecutionID)
			}
		}
		for _, eventID := range item.EvidenceEventIDs {
			if len(evidence) >= limit {
				break
			}
			if _, seen := seenEvidence[eventID]; !seen && eventID != "" {
				seenEvidence[eventID] = struct{}{}
				evidence = append(evidence, eventID)
			}
		}
	}
	sort.Strings(executions)
	sort.Strings(evidence)
	return executions, evidence
}

func mostCommon(groups map[string][]Observation) (string, []Observation) {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var selected string
	var matches []Observation
	for _, key := range keys {
		if len(groups[key]) > len(matches) {
			selected = key
			matches = groups[key]
		}
	}
	return selected, matches
}

func boundedSupportScore(support int, floor int, prevalence float64) string {
	if floor < 1 {
		floor = 1
	}
	supportScore := float64(support) / float64(floor)
	if supportScore > 1 {
		supportScore = 1
	}
	score := 0.35 + 0.35*supportScore + 0.2*prevalence
	if score > 0.9 {
		score = 0.9
	}
	return strconv.FormatFloat(score, 'f', 6, 64)
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func saturatedAnomalyThreshold(median int64, mad int64) int64 {
	spread := maxInt64(mad, 1)
	if spread > math.MaxInt64/6 {
		return math.MaxInt64
	}
	scaled := spread * 6
	if median > math.MaxInt64-scaled {
		return math.MaxInt64
	}
	return median + scaled
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func boundedStatement(prefix string, value string) string {
	statement := prefix + value
	if len(statement) <= maxHypothesisStatementBytes {
		return statement
	}
	digest := sha256.Sum256([]byte(value))
	suffix := " … [truncated sha256:" + hex.EncodeToString(digest[:]) + "]"
	budget := maxHypothesisStatementBytes - len(prefix) - len(suffix)
	if budget < 0 {
		budget = 0
	}
	prefixBytes := []byte(value)
	if len(prefixBytes) > budget {
		prefixBytes = prefixBytes[:budget]
		for len(prefixBytes) > 0 && !utf8.Valid(prefixBytes) {
			prefixBytes = prefixBytes[:len(prefixBytes)-1]
		}
	}
	return prefix + string(prefixBytes) + suffix
}
