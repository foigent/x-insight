package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/workbench"
	"golang.org/x/term"
)

// Personnel queries use the same HTTPS Session, CSRF and current-scope API as
// the browser. They never borrow hostctl/admin credentials or persist a cookie.
type personnelQuery struct {
	origin, username, method, path string
	body                           []byte
}

func personnelQueryUsage() error {
	return errors.New("usage: xinsightctl query budgets create|reading|items|history --origin HTTPS_ORIGIN --username USER [--request-file ABSOLUTE_JSON | --reading ID] [--budget-ref UUID] [--limit 1..100 --cursor CURSOR] | xinsightctl query runtime create|reading|summary|requests|request|associations|resources|measurements|dimensions|observations|runtime|attempts|capabilities|policies|destinations|current --origin HTTPS_ORIGIN --username USER [--request-file ABSOLUTE_JSON | --reading ID] [--entry-time known|unknown|all] [--dimension NAME] [--request-ref REF] [--limit 1..100 --cursor CURSOR] [--source-ids CSV --source-projects CSV --cost-projects CSV]; password is read only from the interactive terminal")
}

func parsePersonnelQuery(args []string) (personnelQuery, error) {
	var q personnelQuery
	if len(args) < 2 || (args[0] != "runtime" && args[0] != "budgets") {
		return q, personnelQueryUsage()
	}
	budget := args[0] == "budgets"
	action := args[1]
	if budget && action != "create" && action != "reading" && action != "items" && action != "history" {
		return q, personnelQueryUsage()
	}
	allowed := map[string]bool{"origin": true, "username": true}
	allow := func(names ...string) {
		for _, name := range names {
			allowed[name] = true
		}
	}
	switch action {
	case "create":
		allow("request-file")
	case "reading":
		allow("reading")
	case "items", "history":
		if !budget {
			return q, personnelQueryUsage()
		}
		allow("reading", "limit", "cursor")
		if action == "history" {
			allow("budget-ref")
		}
	case "summary":
		allow("reading", "entry-time")
	case "requests", "measurements", "dimensions":
		allow("reading", "entry-time", "limit", "cursor")
		if action == "dimensions" {
			allow("dimension")
		}
	case "request", "associations", "resources":
		allow("reading", "request-ref", "limit", "cursor")
	case "observations", "runtime", "attempts", "capabilities", "policies", "destinations":
		allow("reading", "limit", "cursor")
	case "current":
		allow("source-ids", "source-projects", "cost-projects", "limit", "cursor")
	default:
		return q, personnelQueryUsage()
	}
	flags := flag.NewFlagSet("query "+args[0]+" "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&q.origin, "origin", "", "exact HTTPS origin")
	flags.StringVar(&q.username, "username", "", "personnel account name")
	requestFile := flags.String("request-file", "", "runtime reading request JSON")
	reading := flags.String("reading", "", "runtime reading ID")
	entry := flags.String("entry-time", "known", "entry time cohort")
	dimension := flags.String("dimension", "", "dimension")
	budgetRef := flags.String("budget-ref", "", "opaque budget member reference")
	requestRef := flags.String("request-ref", "", "request reference")
	limit := flags.Int("limit", 50, "page size")
	cursor := flags.String("cursor", "", "opaque next cursor")
	sourceIDs := flags.String("source-ids", "", "source UUIDs")
	sourceProjects := flags.String("source-projects", "", "source projects")
	costProjects := flags.String("cost-projects", "", "cost projects")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		return q, personnelQueryUsage()
	}
	validFlags := true
	flags.Visit(func(f *flag.Flag) {
		if !allowed[f.Name] {
			validFlags = false
		}
	})
	if !validFlags {
		return q, personnelQueryUsage()
	}
	u, err := url.Parse(q.origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return q, errors.New("query origin must be an exact HTTPS origin without credentials, path, query or fragment")
	}
	q.origin = "https://" + u.Host
	if q.username == "" || len(q.username) > 128 || strings.ContainsAny(q.username, "\r\n\x00") {
		return q, errors.New("query requires a personnel username")
	}
	q.method = http.MethodGet
	query := url.Values{}
	if allowed["reading"] && ((!budget && !workbench.ValidRuntimeReadingID(*reading)) || (budget && !workbench.ValidBudgetReadingID(*reading))) {
		return q, errors.New("query requires a valid reading ID for its view")
	}
	if allowed["entry-time"] {
		if *entry != "known" && *entry != "unknown" && *entry != "all" {
			return q, errors.New("invalid entry-time cohort")
		}
		query.Set("entry_time", *entry)
	}
	if allowed["limit"] {
		if *limit < 1 || *limit > workbench.MaxPageSize {
			return q, errors.New("query limit must be 1..100")
		}
		query.Set("limit", fmt.Sprint(*limit))
		if *cursor != "" {
			query.Set("cursor", *cursor)
		}
	}
	q.path = "/api/v1/runtime/readings/" + *reading
	if budget {
		q.path = "/api/v1/workbench/budgets/readings/" + *reading
	}
	switch action {
	case "create":
		if !filepath.IsAbs(*requestFile) {
			return q, errors.New("query request-file must be an absolute JSON path")
		}
		before, err := os.Lstat(*requestFile)
		if err != nil || !before.Mode().IsRegular() {
			return q, errors.New("query request-file must be a regular file, not a link")
		}
		f, err := os.OpenFile(*requestFile, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return q, errors.New("cannot open query request-file")
		}
		after, err := f.Stat()
		if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
			f.Close()
			return q, errors.New("query request-file changed while opening")
		}
		raw, readErr := io.ReadAll(io.LimitReader(f, 64*1024+1))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil || len(raw) > 64*1024 {
			return q, errors.New("query request-file cannot be read or exceeds 64 KiB")
		}
		if _, err := connector.DecodeStrictJSONObject(raw); err != nil {
			return q, errors.New("query request-file must be a strict JSON object")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if budget {
			var input workbench.BudgetReadingRequest
			if decoder.Decode(&input) != nil || input.Validate() != nil {
				return q, errors.New("invalid budget reading request version or filters")
			}
			q.method, q.path, q.body = http.MethodPost, "/api/v1/workbench/budgets/readings", raw
		} else {
			var input workbench.RuntimeReadingRequest
			if err := decoder.Decode(&input); err != nil {
				return q, errors.New("invalid runtime reading request fields")
			}
			if _, err := input.Resolve(time.Now().UTC()); err != nil {
				return q, errors.New("invalid runtime reading request version, period or filters")
			}
			q.method, q.path, q.body = http.MethodPost, "/api/v1/runtime/readings", raw
		}
	case "items":
		q.path += "/items"
	case "history":
		if !workbench.ValidBudgetItemRef(*budgetRef) {
			return q, errors.New("query requires a valid budget member reference")
		}
		q.path += "/items/" + *budgetRef + "/history"

	case "current":
		csv := func(s string) []string {
			result := []string{}
			for _, item := range strings.Split(s, ",") {
				if item = strings.TrimSpace(item); item != "" {
					result = append(result, item)
				}
			}
			return result
		}
		filters := workbench.Filters{SourceIDs: csv(*sourceIDs), SourceProjects: csv(*sourceProjects), CostProjects: csv(*costProjects)}
		if err := workbench.ValidateRuntimeCurrentQuery(filters, *limit); err != nil {
			return q, errors.New("invalid current inventory filters")
		}
		for key, values := range map[string][]string{"source_ids": filters.SourceIDs, "source_projects": filters.SourceProjects, "cost_projects": filters.CostProjects} {
			if len(values) > 0 {
				query.Set(key, strings.Join(values, ","))
			}
		}
		q.path = "/api/v1/runtime/current"
	case "dimensions":
		if !workbench.ValidRuntimeDimension(*dimension) {
			return q, errors.New("invalid runtime dimension")
		}
		q.path += "/dimensions/" + *dimension
	case "request", "associations", "resources":
		if !workbench.ValidRuntimeRequestRef(*requestRef) {
			return q, errors.New("invalid runtime request reference")
		}
		q.path += "/requests/" + *requestRef
		if action == "associations" || action == "resources" {
			q.path += "/" + action
		}
	case "reading":
	default:
		q.path += "/" + action
	}
	if len(query) > 0 {
		q.path += "?" + query.Encode()
	}
	return q, nil
}

type personnelSession struct {
	ScopeRevision string `json:"scope_revision"`
	CSRFToken     string `json:"csrf_token"`
	Session       struct {
		SubjectID  string `json:"subject_id"`
		SessionRef string `json:"session_ref"`
	} `json:"session"`
}

type personnelClient struct {
	client              *http.Client
	origin, csrf, scope string
}

// There is no redirect, TLS bypass, retry or fallback to a host/admin identity.
func (c *personnelClient) request(ctx context.Context, method, path string, body []byte, scoped bool) ([]byte, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("cannot construct personnel query")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	req.Header.Set("Origin", c.origin)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		if c.csrf != "" {
			req.Header.Set("X-CSRF-Token", c.csrf)
		}
	}
	if scoped {
		req.Header.Set("X-XInsight-Scope-Revision", c.scope)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, nil, errors.New("personnel HTTPS query failed; verify the approved origin and TLS trust")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Error bodies can contain arbitrary server text, including reflected
		// request data. Emit only status; never credentials or an untrusted title.
		return nil, response, fmt.Errorf("personnel API returned HTTP %d", response.StatusCode)
	}
	if scoped && response.Header.Get("X-XInsight-Scope-Revision") != c.scope {
		return nil, response, errors.New("personnel scope changed; result discarded")
	}
	if response.StatusCode == http.StatusNoContent {
		return nil, response, nil
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return nil, response, errors.New("personnel API did not return JSON")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
	if err != nil || len(raw) > 8*1024*1024 {
		return nil, response, errors.New("personnel API response cannot be read or exceeds 8 MiB")
	}
	if _, err := connector.DecodeStrictJSONObject(raw); err != nil {
		return nil, response, errors.New("personnel API response is not a strict JSON object")
	}
	return raw, response, nil
}

func executePersonnelQuery(ctx context.Context, q personnelQuery, password []byte, transport http.RoundTripper) (result []byte, failure error) {
	defer clear(password)
	if len(password) == 0 || len(password) > 1024 {
		return nil, errors.New("personnel password is empty or too long")
	}
	jar, _ := cookiejar.New(nil)
	client := &personnelClient{origin: q.origin, client: &http.Client{Jar: jar, Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	defer client.client.CloseIdleConnections()
	login, err := json.Marshal(struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{q.username, string(password)})
	if err != nil {
		return nil, errors.New("cannot encode personnel login")
	}
	raw, response, err := client.request(ctx, http.MethodPost, "/api/v1/access/login", login, false)
	clear(login)
	if err != nil {
		return nil, err
	}
	var session personnelSession
	if json.Unmarshal(raw, &session) != nil || session.CSRFToken == "" || session.Session.SubjectID == "" || session.Session.SessionRef == "" {
		return nil, errors.New("personnel login response is incomplete")
	}
	client.csrf = session.CSRFToken
	cookies := 0
	for _, cookie := range response.Cookies() {
		if cookie.Name == access.SessionCookieName {
			if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.Value == "" {
				return nil, errors.New("personnel Session cookie does not satisfy the HTTPS contract")
			}
			cookies++
		}
	}
	if cookies != 1 {
		return nil, errors.New("personnel login must return exactly one Session cookie")
	}
	// This invocation owns exactly this new Session. Always try to end it,
	// including on cancellation or a later authorization failure.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _, err := client.request(cleanup, http.MethodPost, "/api/v1/access/logout", []byte("{}"), false)
		if err != nil {
			result = nil
			failure = errors.Join(failure, errors.New("personnel Session logout was not confirmed"))
		}
	}()
	raw, _, err = client.request(ctx, http.MethodGet, "/api/v1/access/session", nil, false)
	if err != nil {
		return nil, err
	}
	var current personnelSession
	if json.Unmarshal(raw, &current) != nil || current.Session.SubjectID != session.Session.SubjectID || current.Session.SessionRef != session.Session.SessionRef || current.CSRFToken == "" || current.ScopeRevision == "" || current.ScopeRevision == "no_authorized_scope" {
		return nil, errors.New("personnel Session lacks a stable authorized query scope")
	}
	client.csrf, client.scope = current.CSRFToken, current.ScopeRevision
	raw, _, err = client.request(ctx, q.method, q.path, q.body, true)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func runPersonnelQuery(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	q, err := parsePersonnelQuery(args)
	if err != nil {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("personnel query password requires an interactive terminal; argv, environment and piped passwords are not accepted")
	}
	fmt.Fprint(stderr, "Product account password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return errors.New("cannot read personnel password")
	}
	defer clear(password)
	raw, err := executePersonnelQuery(ctx, q, password, http.DefaultTransport)
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err = json.Indent(&pretty, raw, "", "  "); err != nil {
		return errors.New("cannot format query result")
	}
	pretty.WriteByte('\n')
	_, err = stdout.Write(pretty.Bytes())
	return err
}
