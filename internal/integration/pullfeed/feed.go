package pullfeed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/evidence"
)

const (
	SchemaVersion              = "xinsight.completed-execution-feed.v1"
	LiveSnapshotSchemaVersion  = "unirouter.completed-execution.v1"
	LiveSnapshotRevision       = "1"
	MaxPageBytes               = 8 << 20
	MaxPageItems               = 50
	MaxCursorBytes             = 512
	MaxLiveSnapshotBytes       = 64 << 10
	MaxAuthorizationRefBytes   = 256
	DiagnosticVisibilityTarget = 10 * time.Second
)

type Mode string

const (
	ModeDisabled Mode = "disabled"
	ModeMock     Mode = "mock"
)

var (
	ErrDisabled                = errors.New("completed-execution pull feed is disabled")
	ErrLiveContractNotAccepted = errors.New("live completed-execution pull contract is not accepted")
	ErrPollInProgress          = errors.New("completed-execution pull poll is already in progress")
	ErrSourceInstanceMismatch  = errors.New("completed-execution pull source instance does not match activation")
	ErrRevisionConflict        = errors.New("completed-execution pull revision conflicts with an accepted revision")
	ErrRevisionRegression      = errors.New("completed-execution pull revision regressed")
	ErrTimelineRegression      = errors.New("completed-execution pull timeline completeness regressed")
	ErrCursorNoProgress        = errors.New("completed-execution pull cursor made no progress")
	ErrEmptyPageCursorAdvance  = errors.New("completed-execution pull empty page advanced the cursor")
	ErrPreActivationItem       = errors.New("completed-execution pull returned a pre-activation item")
	revisionPattern            = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	sha256Pattern              = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type Config struct {
	Mode       Mode
	Activation Activation
}

type Activation struct {
	SourceInstance   string    `json:"source_instance"`
	ActivatedAt      time.Time `json:"activated_at"`
	Cursor           string    `json:"cursor"`
	AuthorizationRef string    `json:"authorization_ref"`
}

type FeedPage struct {
	SchemaVersion  string     `json:"schema_version"`
	SourceInstance string     `json:"source_instance"`
	GeneratedAt    time.Time  `json:"generated_at"`
	Items          []FeedItem `json:"items"`
	NextCursor     string     `json:"next_cursor"`
	HighWatermark  string     `json:"high_watermark"`
	HasMore        bool       `json:"has_more"`
}

type FeedItem struct {
	RequestID           string          `json:"request_id"`
	TerminalAvailableAt time.Time       `json:"terminal_available_at"`
	Revision            string          `json:"revision"`
	TimelineComplete    bool            `json:"timeline_complete"`
	SnapshotSHA256      string          `json:"snapshot_sha256"`
	Snapshot            json.RawMessage `json:"snapshot"`
}

type Source interface {
	Fetch(context.Context, string, int) ([]byte, error)
}

type CommitResult struct {
	SearchVisibleAt time.Time
}

type Committer interface {
	Commit(context.Context, FeedItem) (CommitResult, error)
}

type CommitFunc func(context.Context, FeedItem) (CommitResult, error)

func (f CommitFunc) Commit(ctx context.Context, item FeedItem) (CommitResult, error) {
	return f(ctx, item)
}

type PollReport struct {
	FromCursor          string
	NextCursor          string
	HighWatermark       string
	ItemsReceived       int
	ItemsCommitted      int
	DuplicateNoop       int
	IncompleteTimelines int
	VisibilityLatencies []time.Duration
}

type acceptedRevision struct {
	SnapshotSHA256      string
	TerminalAvailableAt time.Time
	TimelineComplete    bool
}

type Consumer struct {
	mode          Mode
	activation    Activation
	source        Source
	committer     Committer
	stateMu       sync.RWMutex
	cursor        string
	highWatermark string
	accepted      map[string]map[string]acceptedRevision
	latest        map[string]string
	polling       atomic.Bool
}

func NewConsumer(config Config, source Source, committer Committer) (*Consumer, error) {
	mode := config.Mode
	if mode == "" {
		mode = ModeDisabled
	}

	switch mode {
	case ModeDisabled:
		return &Consumer{mode: mode}, nil
	case ModeMock:
		if err := validateActivation(config.Activation); err != nil {
			return nil, err
		}
		if source == nil {
			return nil, fmt.Errorf("mock source is required")
		}
		if committer == nil {
			return nil, fmt.Errorf("mock committer is required")
		}
		return &Consumer{
			mode:       mode,
			activation: config.Activation,
			source:     source,
			committer:  committer,
			cursor:     config.Activation.Cursor,
			accepted:   make(map[string]map[string]acceptedRevision),
			latest:     make(map[string]string),
		}, nil
	default:
		return nil, ErrLiveContractNotAccepted
	}
}

func (c *Consumer) Cursor() string {
	if c == nil {
		return ""
	}
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.cursor
}

func (c *Consumer) HighWatermark() string {
	if c == nil {
		return ""
	}
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.highWatermark
}

func (c *Consumer) Activation() Activation {
	if c == nil {
		return Activation{}
	}
	return c.activation
}

func (c *Consumer) Poll(ctx context.Context) (PollReport, error) {
	if c == nil || c.mode == ModeDisabled {
		return PollReport{}, ErrDisabled
	}
	if !c.polling.CompareAndSwap(false, true) {
		return PollReport{}, ErrPollInProgress
	}
	defer c.polling.Store(false)

	fromCursor := c.Cursor()
	payload, err := c.source.Fetch(ctx, fromCursor, MaxPageItems)
	if err != nil {
		return PollReport{FromCursor: fromCursor}, fmt.Errorf("fetch mock feed: %w", err)
	}
	page, err := DecodePage(payload)
	if err != nil {
		return PollReport{FromCursor: fromCursor}, err
	}
	if page.SourceInstance != c.activation.SourceInstance {
		return PollReport{FromCursor: fromCursor}, fmt.Errorf(
			"%w: activated=%q received=%q",
			ErrSourceInstanceMismatch,
			c.activation.SourceInstance,
			page.SourceInstance,
		)
	}
	if page.GeneratedAt.Before(c.activation.ActivatedAt) {
		return PollReport{FromCursor: fromCursor}, ErrPreActivationItem
	}
	if page.HasMore && page.NextCursor == fromCursor {
		return PollReport{FromCursor: fromCursor}, ErrCursorNoProgress
	}

	report := PollReport{
		FromCursor:          fromCursor,
		NextCursor:          page.NextCursor,
		HighWatermark:       page.HighWatermark,
		ItemsReceived:       len(page.Items),
		VisibilityLatencies: make([]time.Duration, 0, len(page.Items)),
	}
	for _, item := range page.Items {
		if item.TerminalAvailableAt.Before(c.activation.ActivatedAt) {
			return report, fmt.Errorf("%w: request_id=%q", ErrPreActivationItem, item.RequestID)
		}
		duplicate, revisionErr := c.classifyRevision(item)
		if revisionErr != nil {
			return report, revisionErr
		}
		if duplicate {
			report.DuplicateNoop++
			if !item.TimelineComplete {
				report.IncompleteTimelines++
			}
			continue
		}
		result, commitErr := c.committer.Commit(ctx, item)
		if commitErr != nil {
			return report, fmt.Errorf("commit mock feed item: %w", commitErr)
		}
		if result.SearchVisibleAt.IsZero() || result.SearchVisibleAt.Before(item.TerminalAvailableAt) {
			return report, fmt.Errorf("commit result has invalid search_visible_at")
		}
		report.ItemsCommitted++
		report.VisibilityLatencies = append(
			report.VisibilityLatencies,
			result.SearchVisibleAt.Sub(item.TerminalAvailableAt),
		)
		if !item.TimelineComplete {
			report.IncompleteTimelines++
		}
		c.acceptRevision(item)
	}

	c.stateMu.Lock()
	c.cursor = page.NextCursor
	c.highWatermark = page.HighWatermark
	c.stateMu.Unlock()
	return report, nil
}

func (c *Consumer) classifyRevision(item FeedItem) (bool, error) {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	requestRevisions := c.accepted[item.RequestID]
	if accepted, exists := requestRevisions[item.Revision]; exists {
		if accepted.SnapshotSHA256 != item.SnapshotSHA256 ||
			!accepted.TerminalAvailableAt.Equal(item.TerminalAvailableAt) ||
			accepted.TimelineComplete != item.TimelineComplete {
			return false, fmt.Errorf("%w: request_id=%q revision=%q", ErrRevisionConflict, item.RequestID, item.Revision)
		}
		return true, nil
	}

	latestRevision, exists := c.latest[item.RequestID]
	if !exists {
		return false, nil
	}
	comparison := compareRevision(item.Revision, latestRevision)
	if comparison < 0 {
		return false, fmt.Errorf(
			"%w: request_id=%q latest=%q received=%q",
			ErrRevisionRegression,
			item.RequestID,
			latestRevision,
			item.Revision,
		)
	}
	latest := requestRevisions[latestRevision]
	if comparison > 0 && latest.TimelineComplete && !item.TimelineComplete {
		return false, fmt.Errorf(
			"%w: request_id=%q latest=%q received=%q",
			ErrTimelineRegression,
			item.RequestID,
			latestRevision,
			item.Revision,
		)
	}
	if !latest.TerminalAvailableAt.Equal(item.TerminalAvailableAt) {
		return false, fmt.Errorf(
			"%w: terminal_available_at changed for request_id=%q",
			ErrRevisionConflict,
			item.RequestID,
		)
	}
	return false, nil
}

func (c *Consumer) acceptRevision(item FeedItem) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.accepted[item.RequestID] == nil {
		c.accepted[item.RequestID] = make(map[string]acceptedRevision)
	}
	c.accepted[item.RequestID][item.Revision] = acceptedRevision{
		SnapshotSHA256:      item.SnapshotSHA256,
		TerminalAvailableAt: item.TerminalAvailableAt,
		TimelineComplete:    item.TimelineComplete,
	}
	latest, exists := c.latest[item.RequestID]
	if !exists || compareRevision(item.Revision, latest) > 0 {
		c.latest[item.RequestID] = item.Revision
	}
}

func DecodePage(payload []byte) (FeedPage, error) {
	return decodePage(payload, true)
}

// DecodeLivePage validates the accepted production V1 page envelope and its
// byte hashes. The X-Insight-owned UniRouter adapter performs the strict inner
// DTO decode and source-neutral evidence mapping after this boundary.
func DecodeLivePage(payload []byte) (FeedPage, error) {
	page, err := decodePage(payload, false)
	if err != nil {
		return FeedPage{}, err
	}
	for index, item := range page.Items {
		if len(item.RequestID) > 64 {
			return FeedPage{}, fmt.Errorf("items[%d]: live request_id exceeds 64 bytes", index)
		}
		if item.Revision != LiveSnapshotRevision {
			return FeedPage{}, fmt.Errorf("items[%d]: live revision must be %q", index, LiveSnapshotRevision)
		}
		if !item.TimelineComplete {
			return FeedPage{}, fmt.Errorf("items[%d]: live timeline_complete must be true", index)
		}
		if len(item.Snapshot) > MaxLiveSnapshotBytes {
			return FeedPage{}, fmt.Errorf("items[%d]: live snapshot exceeds %d bytes", index, MaxLiveSnapshotBytes)
		}
	}
	return page, nil
}

func decodePage(payload []byte, requireMockEnvelope bool) (FeedPage, error) {
	if len(payload) == 0 {
		return FeedPage{}, fmt.Errorf("feed page is empty")
	}
	if len(payload) > MaxPageBytes {
		return FeedPage{}, fmt.Errorf("feed page exceeds %d bytes", MaxPageBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var page FeedPage
	if err := decoder.Decode(&page); err != nil {
		return FeedPage{}, fmt.Errorf("decode feed page: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return FeedPage{}, fmt.Errorf("feed page contains trailing content")
	}
	if err := validatePage(page, requireMockEnvelope); err != nil {
		return FeedPage{}, err
	}
	return page, nil
}

func VisibilityP95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := (95*len(ordered) + 99) / 100
	return ordered[index-1]
}

func MeetsDiagnosticVisibilityTarget(values []time.Duration) bool {
	return len(values) > 0 && VisibilityP95(values) <= DiagnosticVisibilityTarget
}

func compareRevision(left string, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func validateActivation(activation Activation) error {
	if err := validateBoundedText("activation source_instance", activation.SourceInstance, 1, 128); err != nil {
		return err
	}
	if activation.ActivatedAt.IsZero() {
		return fmt.Errorf("activation activated_at is required")
	}
	if err := validateBoundedText("activation cursor", activation.Cursor, 1, MaxCursorBytes); err != nil {
		return err
	}
	if err := validateBoundedText("activation authorization_ref", activation.AuthorizationRef, 1, MaxAuthorizationRefBytes); err != nil {
		return err
	}
	return nil
}

func validatePage(page FeedPage, requireMockEnvelope bool) error {
	if page.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported feed schema_version")
	}
	if err := validateBoundedText("source_instance", page.SourceInstance, 1, 128); err != nil {
		return err
	}
	if page.GeneratedAt.IsZero() {
		return fmt.Errorf("generated_at is required")
	}
	if len(page.Items) > MaxPageItems {
		return fmt.Errorf("feed page exceeds %d items", MaxPageItems)
	}
	if len(page.NextCursor) > MaxCursorBytes || len(page.HighWatermark) > MaxCursorBytes {
		return fmt.Errorf("feed cursor or watermark exceeds %d bytes", MaxCursorBytes)
	}
	if err := validateBoundedText("next_cursor", page.NextCursor, 1, MaxCursorBytes); err != nil {
		return err
	}
	if err := validateBoundedText("high_watermark", page.HighWatermark, 1, MaxCursorBytes); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(page.Items))
	latestInPage := make(map[string]FeedItem, len(page.Items))
	for index, item := range page.Items {
		if err := validateItem(item, requireMockEnvelope); err != nil {
			return fmt.Errorf("items[%d]: %w", index, err)
		}
		if requireMockEnvelope && page.GeneratedAt.Before(item.TerminalAvailableAt) {
			return fmt.Errorf("items[%d]: terminal_available_at is after generated_at", index)
		}
		key := item.RequestID + "\x00" + item.Revision
		if _, exists := seen[key]; exists {
			return fmt.Errorf("items[%d]: duplicate request_id and revision", index)
		}
		seen[key] = struct{}{}
		if prior, exists := latestInPage[item.RequestID]; exists {
			if compareRevision(item.Revision, prior.Revision) <= 0 {
				return fmt.Errorf("items[%d]: request revision order is not increasing", index)
			}
			if !item.TerminalAvailableAt.Equal(prior.TerminalAvailableAt) {
				return fmt.Errorf("items[%d]: terminal_available_at changed across request revisions", index)
			}
			if prior.TimelineComplete && !item.TimelineComplete {
				return fmt.Errorf("items[%d]: timeline completeness regressed across request revisions", index)
			}
		}
		latestInPage[item.RequestID] = item
	}
	return nil
}

func validateItem(item FeedItem, requireMockEnvelope bool) error {
	if err := validateBoundedText("request_id", item.RequestID, 1, 256); err != nil {
		return err
	}
	if item.TerminalAvailableAt.IsZero() {
		return fmt.Errorf("terminal_available_at is required")
	}
	if !revisionPattern.MatchString(item.Revision) {
		return fmt.Errorf("revision must be a positive unsigned decimal string")
	}
	if !sha256Pattern.MatchString(item.SnapshotSHA256) {
		return fmt.Errorf("snapshot_sha256 is invalid")
	}
	if len(item.Snapshot) == 0 || bytes.Equal(bytes.TrimSpace(item.Snapshot), []byte("null")) {
		return fmt.Errorf("snapshot is required")
	}
	var object map[string]any
	if err := json.Unmarshal(item.Snapshot, &object); err != nil || object == nil {
		return fmt.Errorf("snapshot must be a JSON object")
	}
	if requireMockEnvelope {
		if err := validateMockSnapshotEnvelope(object, item.RequestID); err != nil {
			return err
		}
	}
	if findings := evidence.DetectSensitive(object); len(findings) > 0 {
		return fmt.Errorf("snapshot contains disallowed sensitive field at %s", findings[0].Path)
	}
	digest := sha256.Sum256(item.Snapshot)
	actual := "sha256:" + hex.EncodeToString(digest[:])
	if actual != item.SnapshotSHA256 {
		return fmt.Errorf("snapshot_sha256 does not match snapshot bytes")
	}
	return nil
}

func validateMockSnapshotEnvelope(snapshot map[string]any, requestID string) error {
	schemaVersion, ok := snapshot["schema_version"].(string)
	if !ok {
		return fmt.Errorf("snapshot schema_version is required")
	}
	if err := validateBoundedText("snapshot schema_version", schemaVersion, 1, 128); err != nil {
		return err
	}
	requestIDs, ok := snapshot["request_ids"].([]any)
	if !ok || len(requestIDs) != 1 {
		return fmt.Errorf("mock snapshot must declare exactly one request_id")
	}
	declaredRequestID, ok := requestIDs[0].(string)
	if !ok || declaredRequestID != requestID {
		return fmt.Errorf("mock snapshot request_id does not match feed item")
	}
	safety, ok := snapshot["safety"].(map[string]any)
	if !ok {
		return fmt.Errorf("mock snapshot safety declaration is required")
	}
	for _, name := range []string{"raw_prompts_included", "raw_responses_included", "api_keys_included"} {
		value, exists := safety[name]
		if !exists {
			return fmt.Errorf("mock snapshot safety.%s is required", name)
		}
		included, ok := value.(bool)
		if !ok || included {
			return fmt.Errorf("mock snapshot safety.%s must be false", name)
		}
	}
	return nil
}

func validateBoundedText(name string, value string, minimum int, maximum int) error {
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain surrounding whitespace", name)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", name)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain Unicode control characters", name)
		}
	}
	length := len([]byte(value))
	if length < minimum || length > maximum {
		return fmt.Errorf("%s must contain %d to %d UTF-8 bytes", name, minimum, maximum)
	}
	return nil
}
