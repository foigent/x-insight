package hostctl

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in fixture exercises an installed OpenClaw Gateway against the
// actual XI HTTPS handler and restricted PostgreSQL receiver. It never loads
// the user's OpenClaw state, a real model provider, or a real credential.
func runSyntheticOpenClawGateway(t *testing.T, xi *httptest.Server, token, workspaceDir string, admin *sql.DB) {
	t.Helper()
	if _, err := exec.LookPath("openclaw"); err != nil {
		t.Fatal("OpenClaw Gateway opt-in test requires the installed CLI:", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	var upstreamCalls atomic.Int32
	var overlapped atomic.Bool
	var builtinCalls atomic.Int32
	var builtinEditObserved atomic.Bool
	builtinFile := filepath.Join(workspaceDir, "xi-builtin-chain.txt")
	missingFile := filepath.Join(workspaceDir, "xi-builtin-missing.txt")
	if err := os.WriteFile(builtinFile, []byte("alpha\n"), 0600); err != nil {
		t.Fatal(err)
	}
	secondRequest := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		requestBody, readErr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if readErr != nil {
			http.Error(w, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		builtin := strings.Contains(string(requestBody), "XI_BUILTIN_CHAIN")
		step := upstreamCalls.Add(1)
		if step == 2 {
			close(secondRequest)
		}
		if step == 1 {
			select {
			case <-secondRequest:
				overlapped.Store(true)
			case <-time.After(2 * time.Second):
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		if step <= 2 {
			w.Header().Set("X-Request-ID", fmt.Sprintf("op_%032x", step))
		}
		w.WriteHeader(http.StatusOK)
		if builtin {
			index := builtinCalls.Add(1)
			switch index {
			case 1:
				writeSyntheticGatewayToolCall(w, "read", map[string]string{"path": builtinFile}, index)
			case 2:
				writeSyntheticGatewayToolCall(w, "edit", map[string]string{"path": builtinFile, "oldText": "alpha", "newText": "beta"}, index)
			case 3:
				if edited, err := os.ReadFile(builtinFile); err == nil && string(edited) == "beta\n" {
					builtinEditObserved.Store(true)
				}
				writeSyntheticGatewayToolCall(w, "write", map[string]string{"path": builtinFile, "content": "gamma\n"}, index)
			case 4:
				writeSyntheticGatewayToolCall(w, "read", map[string]string{"path": builtinFile}, index)
			case 5:
				writeSyntheticGatewayToolCall(w, "read", map[string]string{"path": missingFile}, index)
			default:
				writeSyntheticGatewayFinal(w)
			}
		} else if step == 1 {
			_, _ = fmt.Fprint(w, `data: {"id":"chatcmpl-xi","object":"chat.completion.chunk","created":1790629000,"model":"synthetic","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_xi_probe_1","type":"function","function":{"name":"xi_probe_tool","arguments":"{}"}}]},"finish_reason":null}]}`+"\n\n")
			_, _ = fmt.Fprint(w, `data: {"id":"chatcmpl-xi","object":"chat.completion.chunk","created":1790629000,"model":"synthetic","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			writeSyntheticGatewayFinal(w)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	toolDir := filepath.Join(root, "tool-plugin")
	if err := os.MkdirAll(toolDir, 0700); err != nil {
		t.Fatal(err)
	}
	toolFiles := map[string]string{
		"package.json":         `{"name":"xi-synthetic-tool","version":"0.0.0","type":"module","main":"index.mjs"}`,
		"openclaw.plugin.json": `{"id":"xi-synthetic-tool","name":"XI Synthetic Tool","description":"Local test tool","categories":["other"],"contracts":{"tools":["xi_probe_tool"]},"activation":{"onStartup":true},"configSchema":{"type":"object","additionalProperties":false}}`,
		"index.mjs":            `export default { id: "xi-synthetic-tool", name: "XI Synthetic Tool", register(api) { api.registerTool({ name: "xi_probe_tool", description: "Synthetic no-op", parameters: { type: "object", properties: {}, additionalProperties: false }, async execute() { return { content: [{ type: "text", text: "xi-tool-ok" }] }; } }); } };`,
	}
	for name, body := range toolFiles {
		if err := os.WriteFile(filepath.Join(toolDir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pluginDir, err := filepath.Abs("../../integrations/openclaw-xinsight")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	model := map[string]any{"id": "synthetic", "name": "Synthetic", "reasoning": false,
		"input": []string{"text"}, "cost": map[string]int{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
		"contextWindow": 128000, "maxTokens": 4096}
	// Preserve the pre-existing custom provider/model identity. The managed
	// observer must obtain the response header through public hook aliases.
	tokenFile, bodyKeyFile := filepath.Join(root, "ingest.key"), filepath.Join(root, "body.key")
	for path, value := range map[string]string{tokenFile: token, bodyKeyFile: strings.Repeat("07", 32)} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	settings := map[string]any{
		"plugins": map[string]any{"allow": []string{"xi-work-events", "xi-synthetic-tool"},
			"load": map[string]any{"paths": []string{pluginDir, toolDir}},
			"entries": map[string]any{"xi-work-events": map[string]any{"enabled": true,
				"hooks": map[string]any{"allowConversationAccess": true},
				"config": map[string]any{"origin": xi.URL, "outboxPath": filepath.Join(root, "outbox", "queue.sqlite"),
					"bodyOutboxPath": filepath.Join(root, "body-outbox", "queue.sqlite"),
					"tokenFile":      tokenFile, "bodyKeyFile": bodyKeyFile,
					"syntheticLoopback": true, "managedProvider": map[string]any{
						"baseUrl": upstream.URL + "/v1"}}},
				"xi-synthetic-tool": map[string]any{"enabled": true}}},
		"gateway": map[string]any{"mode": "local", "bind": "loopback", "port": port,
			"auth": map[string]any{"mode": "none"}},
		"agents": map[string]any{"defaults": map[string]any{"model": map[string]any{"primary": "unirouter-existing/synthetic"},
			"workspace": workspaceDir}},
		"models": map[string]any{"mode": "merge", "catalogRefresh": map[string]any{"enabled": false},
			"providers": map[string]any{"unirouter-existing": map[string]any{"baseUrl": upstream.URL + "/v1",
				"apiKey": "synthetic-local-only", "api": "openai-completions", "models": []any{model}}}},
	}
	configRaw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "openclaw.json")
	if err := os.WriteFile(configPath, configRaw, 0600); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(root, "xi-local-cert.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: xi.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root,
		"TMPDIR=" + filepath.Join(root, "tmp"), "OPENCLAW_STATE_DIR=" + stateDir,
		"OPENCLAW_CONFIG_PATH=" + configPath, "NODE_EXTRA_CA_CERTS=" + certPath}
	logs, err := os.Create(filepath.Join(root, "gateway.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	gateway := exec.CommandContext(ctx, "openclaw", "gateway", "run", "--port", fmt.Sprint(port))
	gateway.Env = env
	gateway.Stdout, gateway.Stderr = logs, logs
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gateway.Process.Kill(); _ = gateway.Wait() }()
	ready := false
	for i := 0; i < 200; i++ {
		conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("isolated Gateway did not listen: %s", gatewayLogTail(root))
	}
	time.Sleep(500 * time.Millisecond)
	results := make(chan error, 2)
	for _, session := range []string{"agent:main:xi-p2-a", "agent:main:xi-p2-b"} {
		go func(key string) {
			command := exec.CommandContext(ctx, "openclaw", "agent", "--session-key", key,
				"--message", "Use the synthetic local response", "--json", "--timeout", "45")
			command.Env = env
			output, callErr := command.CombinedOutput()
			if callErr != nil {
				results <- fmt.Errorf("agent %s: %w: %.1500s", key, callErr, output)
				return
			}
			if !strings.Contains(string(output), `"status": "ok"`) {
				results <- fmt.Errorf("agent %s did not complete: %.1500s", key, output)
				return
			}
			results <- nil
		}(session)
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("%v; Gateway log: %s", err, gatewayLogTail(root))
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	var events, relations, tools, tasks, modelStarts, modelEnds, distinctRuns int
	for time.Now().Before(deadline) {
		err := admin.QueryRow(`SELECT count(*),count(*) FILTER (WHERE subject_kind='tool_call'),
		  count(*) FILTER (WHERE subject_kind='task'),
		  count(*) FILTER (WHERE action_kind='model.requested'),
		  count(*) FILTER (WHERE action_kind IN ('model.completed','model.failed')),
		  count(DISTINCT run_id)
		  FROM work_event_documents WHERE event_ref LIKE 'oc:%'`).Scan(&events, &tools, &tasks, &modelStarts, &modelEnds, &distinctRuns)
		if err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRow(`SELECT count(*) FROM work_event_relations WHERE relation_ref LIKE 'ur:%'`).Scan(&relations); err != nil {
			t.Fatal(err)
		}
		if events >= 7 && tools >= 2 && tasks >= 2 && modelStarts >= 3 && distinctRuns >= 2 &&
			relations == 2 && overlapped.Load() {
			var copiedBodies, modelInputs, modelOutputs int
			if err := admin.QueryRow(`SELECT count(*),count(*) FILTER(WHERE content_kind='model_input'),
 count(*) FILTER(WHERE content_kind='model_output') FROM review_body_operations
 WHERE source_kind='work_event' AND state='accepted'`).Scan(&copiedBodies, &modelInputs, &modelOutputs); err != nil {
				t.Fatal(err)
			}
			if copiedBodies < 2 || modelInputs < 2 || modelOutputs < 2 {
				time.Sleep(150 * time.Millisecond)
				continue
			}
			var wrong int
			if err := admin.QueryRow(`SELECT count(*) FROM work_event_relations r
			  LEFT JOIN work_event_documents e ON e.source_system_id=r.reporter_source_system_id
			  AND e.source_instance=r.reporter_source_instance AND e.org_id=r.reporter_org_id
			  AND e.project_id=r.reporter_project_id AND e.event_ref=r.event_ref AND e.revision=r.event_revision
			  WHERE r.relation_ref LIKE 'ur:%' AND (e.event_hash IS DISTINCT FROM r.event_hash OR
			  r.target_request_id NOT IN ('op_00000000000000000000000000000001','op_00000000000000000000000000000002'))`).Scan(&wrong); err != nil {
				t.Fatal(err)
			}
			if wrong != 0 {
				t.Fatalf("Gateway relation mismatched own event/hash/header: %d", wrong)
			}
			t.Logf("isolated Gateway: events=%d linked_relations=%d accepted_review_bodies=%d model_inputs=%d model_outputs=%d distinct_runs=%d", events, relations, copiedBodies, modelInputs, modelOutputs, distinctRuns)
			if os.Getenv("XINSIGHT_OPENCLAW_BUILTIN_CHAIN_TEST") == "1" {
				runBuiltinFileChain(t, ctx, env, builtinFile, missingFile, admin, &builtinCalls, &builtinEditObserved, root)
			}
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	localState, _ := exec.Command("sqlite3", "-json", filepath.Join(root, "outbox", "queue.sqlite"),
		"SELECT call_id,run_id,event_ref,ur_request_id,response_status,ambiguous,header_context_json FROM correlations").CombinedOutput()
	queueState, _ := exec.Command("sqlite3", "-json", filepath.Join(root, "outbox", "queue.sqlite"),
		"SELECT kind,state,last_error FROM items").CombinedOutput()
	bodyQueueState, _ := exec.Command("sqlite3", "-json", filepath.Join(root, "body-outbox", "queue.sqlite"),
		"SELECT state,reason,count(*) AS count FROM bodies GROUP BY state,reason").CombinedOutput()
	var copiedBodies int
	_ = admin.QueryRow(`SELECT count(*) FROM review_body_operations WHERE source_kind='work_event' AND state='accepted'`).Scan(&copiedBodies)
	t.Fatalf("Gateway observations missing: upstream=%d events=%d relations=%d tools=%d tasks=%d model_starts=%d model_ends=%d runs=%d bodies=%d correlation=%s queue=%s body_queue=%s log=%s",
		upstreamCalls.Load(), events, relations, tools, tasks, modelStarts, modelEnds, distinctRuns, copiedBodies, localState, queueState, bodyQueueState, gatewayLogTail(root))
}

func writeSyntheticGatewayToolCall(w io.Writer, name string, args map[string]string, index int32) {
	arguments, _ := json.Marshal(args)
	chunk := map[string]any{"id": "chatcmpl-xi", "object": "chat.completion.chunk", "created": 1790629000,
		"model": "synthetic", "choices": []any{map[string]any{"index": 0,
			"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0,
				"id": fmt.Sprintf("call_xi_builtin_%d", index), "type": "function",
				"function": map[string]string{"name": name, "arguments": string(arguments)}}}},
			"finish_reason": nil}}}
	raw, _ := json.Marshal(chunk)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	_, _ = fmt.Fprint(w, `data: {"id":"chatcmpl-xi","object":"chat.completion.chunk","created":1790629000,"model":"synthetic","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
}

func writeSyntheticGatewayFinal(w io.Writer) {
	_, _ = fmt.Fprint(w, `data: {"id":"chatcmpl-xi","object":"chat.completion.chunk","created":1790629000,"model":"synthetic","choices":[{"index":0,"delta":{"content":"xi-model-ok"},"finish_reason":null}]}`+"\n\n")
	_, _ = fmt.Fprint(w, `data: {"id":"chatcmpl-xi","object":"chat.completion.chunk","created":1790629000,"model":"synthetic","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
}

func runBuiltinFileChain(t *testing.T, ctx context.Context, env []string, path, missingPath string, admin *sql.DB,
	calls *atomic.Int32, editObserved *atomic.Bool, root string) {
	t.Helper()
	command := exec.CommandContext(ctx, "openclaw", "agent", "--session-key", "agent:main:xi-p2-builtin",
		"--message", "XI_BUILTIN_CHAIN: perform the requested file operations", "--json", "--timeout", "45")
	command.Env = env
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), `"status": "ok"`) {
		t.Fatalf("builtin agent did not complete: %v %.1500s; Gateway log: %s", err, output, gatewayLogTail(root))
	}
	if calls.Load() < 6 {
		t.Fatalf("builtin chain stopped early after %d model calls: %.1500s", calls.Load(), output)
	}
	if !editObserved.Load() {
		t.Fatalf("builtin edit did not change the file before write: %.1500s", output)
	}
	file, err := os.ReadFile(path)
	if err != nil || string(file) != "gamma\n" {
		t.Fatalf("builtin read/edit/write/read result=%q error=%v", file, err)
	}
	if _, err := os.Stat(missingPath); !os.IsNotExist(err) {
		t.Fatalf("missing-file read unexpectedly created content: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var reads, edits, writes, failedReads, failedTools int
		err = admin.QueryRow(`SELECT count(*) FILTER (WHERE action_kind='resource.read'),
 count(*) FILTER (WHERE action_kind='resource.edit'),count(*) FILTER (WHERE action_kind='resource.write'),
 count(*) FILTER (WHERE action_kind='resource.read.failed' AND resource_name=$2)
 FROM work_event_documents WHERE subject_kind='resource_action' AND resource_name IN ($1,$2)`, path, missingPath).
			Scan(&reads, &edits, &writes, &failedReads)
		if err != nil {
			t.Fatal(err)
		}
		if err = admin.QueryRow(`SELECT count(*) FROM work_event_documents WHERE subject_kind='tool_call'
 AND action_kind='tool.failed' AND resource_name='read'`).Scan(&failedTools); err != nil {
			t.Fatal(err)
		}
		if reads >= 2 && edits >= 1 && writes >= 1 && failedReads >= 1 && failedTools >= 1 {
			t.Logf("installed OpenClaw builtin file chain: reads=%d edits=%d writes=%d failed_reads=%d failed_tools=%d model_calls=%d", reads, edits, writes, failedReads, failedTools, calls.Load())
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	rows, queryErr := admin.Query(`SELECT action_kind,resource_name FROM work_event_documents
 WHERE subject_kind IN ('tool_call','resource_action') ORDER BY received_at DESC LIMIT 30`)
	var observed []string
	if queryErr == nil {
		defer rows.Close()
		for rows.Next() {
			var action, resource string
			if rows.Scan(&action, &resource) == nil {
				observed = append(observed, action+" "+resource)
			}
		}
	}
	t.Fatalf("builtin file chain reached disk but not XI work events; observed=%q; Gateway log: %s", observed, gatewayLogTail(root))
}

func gatewayLogTail(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "gateway.log"))
	if err != nil {
		return err.Error()
	}
	if len(raw) > 8000 {
		raw = raw[len(raw)-8000:]
	}
	return string(raw)
}
