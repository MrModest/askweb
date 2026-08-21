package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MrModest/askweb/internal/server"
	"github.com/MrModest/askweb/internal/whitelist"
)

// newOrigin starts a local TLS server and returns an http.Client whose dialer
// sends every connection to it, whatever hostname was requested.
// Tests can therefore use realistic https://example.com URLs — httptest's
// built-in certificate is valid for example.com — while every byte stays
// in-process and no test ever reaches the real network.
func newOrigin(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	origin := httptest.NewTLSServer(handler)
	t.Cleanup(origin.Close)

	addr := strings.TrimPrefix(origin.URL, "https://")
	transport := origin.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: transport}
}

// writeWhitelist writes entries to a temp whitelist file and returns its path.
func writeWhitelist(t *testing.T, entries ...string) string {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshalling whitelist: %v", err)
	}
	path := filepath.Join(t.TempDir(), "whitelist.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing whitelist: %v", err)
	}
	return path
}

// elicitHandler answers an approval prompt. A nil handler means the client
// declares no elicitation capability at all.
type elicitHandler func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)

// connect wires a client that cannot answer prompts to a server built over the
// given whitelist entries.
func connect(t *testing.T, client *http.Client, entries ...string) *mcp.ClientSession {
	t.Helper()
	return connectWith(t, client, nil, entries...)
}

// connectWith is connect with a scripted answer to any approval prompt, using
// in-memory transports so no MCP traffic touches the network.
func connectWith(t *testing.T, client *http.Client, answer elicitHandler, entries ...string) *mcp.ClientSession {
	t.Helper()
	store, _ := loadStore(t, entries...)
	return connectTo(t, client, answer, store)
}

// loadStore builds a store over a fresh whitelist file and returns both, for
// tests that need to inspect what gets saved.
func loadStore(t *testing.T, entries ...string) (*whitelist.Store, string) {
	t.Helper()
	path := writeWhitelist(t, entries...)
	store, err := whitelist.Load(path)
	if err != nil {
		t.Fatalf("loading whitelist: %v", err)
	}
	return store, path
}

// connectTo is connectWith over a store the caller already built, for tests
// that need to control or inspect the whitelist file.
func connectTo(t *testing.T, client *http.Client, answer elicitHandler, store *whitelist.Store) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.New(store, client).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"},
		&mcp.ClientOptions{ElicitationHandler: answer}).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting client: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

func callWebFetch(t *testing.T, session *mcp.ClientSession, url string) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "web_fetch",
		Arguments: map[string]any{"url": url},
	})
	if err != nil {
		t.Fatalf("CallTool returned a protocol error: %v", err)
	}
	return res
}

// tryWebFetch is callWebFetch for paths that are expected to refuse, where the
// refusal may surface as a protocol error instead of a tool error.
func tryWebFetch(session *mcp.ClientSession, url string) (*mcp.CallToolResult, error) {
	return session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "web_fetch",
		Arguments: map[string]any{"url": url},
	})
}

// refused reports whether a call was refused, in either of the two shapes.
func refused(res *mcp.CallToolResult, err error) bool {
	return err != nil || res.IsError
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, content := range res.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			t.Fatalf("unexpected content type %T", content)
		}
		sb.WriteString(text.Text)
	}
	return sb.String()
}

// The other tests use in-memory transports, which skip the HTTP layer
// entirely. This one connects over Streamable HTTP at /mcp, the way a real
// client does, so the transport wiring is covered too (ADR-0004).
func TestServesOverStreamableHTTPAtMCPPath(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello over http"))
	})
	store, err := whitelist.Load(writeWhitelist(t, "example.com"))
	if err != nil {
		t.Fatalf("loading whitelist: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server.New(store, client) },
		nil,
	))
	mcpServer := httptest.NewServer(mux)
	defer mcpServer.Close()

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{
			Endpoint: mcpServer.URL + "/mcp",
		}, nil)
	if err != nil {
		t.Fatalf("connecting over Streamable HTTP: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	var found bool
	for _, tool := range tools.Tools {
		if tool.Name == "web_fetch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("web_fetch not advertised over HTTP; got %v", tools.Tools)
	}

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != "hello over http" {
		t.Errorf("body = %q, want %q", got, "hello over http")
	}
}

func TestWebFetchIsAdvertisedInTheToolList(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {})
	session := connect(t, client)

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "web_fetch" {
			return
		}
	}
	t.Errorf("web_fetch not found in tool list %v", res.Tools)
}

func TestWebFetchReturnsBodyForWhitelistedHost(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from example.com"))
	})
	session := connect(t, client, "example.com")

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != "hello from example.com" {
		t.Errorf("body = %q, want %q", got, "hello from example.com")
	}
}

// The whole point of the gate is that a refused URL is never requested. The
// origin here is a tripwire: any hit at all fails the test.
func TestWebFetchRefusesAndFetchesNothingForNonWhitelistedURLs(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"unknown host", "https://evil.com/page"},
		{"subdomain of whitelisted host", "https://sub.example.com/page"},
		{"suffix-extended host", "https://example.com.evil.com/page"},
		{"prefix-extended host", "https://evil-example.com/page"},
		{"homograph of whitelisted host", "https://аpple.com/page"},
		{"ftp scheme", "ftp://example.com/file"},
		{"file scheme", "file:///etc/passwd"},
		{"no host", "https:///page"},
		{"not a URL", "nonsense"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fetched bool
			client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
				fetched = true
			})
			session := connect(t, client, "example.com", "apple.com")

			res, err := tryWebFetch(session, tt.url)
			if !refused(res, err) {
				t.Errorf("CallTool(%q) succeeded, want a refusal", tt.url)
			}
			if fetched {
				t.Errorf("CallTool(%q) performed a fetch, want none", tt.url)
			}
		})
	}
}

// A refusal names the blocked host so the operator knows what to whitelist, and
// discloses nothing else — not the whitelist contents, not internal state.
func TestRefusalNamesOnlyTheBlockedHost(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {})
	session := connect(t, client, "example.com", "secret-internal.example.org")

	res := callWebFetch(t, session, "https://evil.com/page")
	text := resultText(t, res)

	if !strings.Contains(text, "evil.com") {
		t.Errorf("refusal %q does not name the blocked host", text)
	}
	if !strings.Contains(text, "not approved") {
		t.Errorf("refusal %q is not an approval refusal", text)
	}
	for _, entry := range []string{"example.com", "secret-internal.example.org"} {
		if strings.Contains(text, entry) {
			t.Errorf("refusal %q leaks whitelist entry %q", text, entry)
		}
	}
}

func TestWebFetchReportsUpstreamHTTPErrorStatus(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	session := connect(t, client, "example.com")

	res := callWebFetch(t, session, "https://example.com/missing")
	if !res.IsError {
		t.Fatal("CallTool succeeded on a 404, want an error")
	}
	if text := resultText(t, res); !strings.Contains(text, "404") {
		t.Errorf("error %q does not mention the 404 status", text)
	}
}

// --- Ticket 02: approving an unknown host -------------------------------

// answer builds a handler that accepts the prompt with the given choice, and
// records the prompt it saw.
func answer(choice string, seen **mcp.ElicitParams) elicitHandler {
	return func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		if seen != nil {
			*seen = req.Params
		}
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": choice}}, nil
	}
}

func TestUnknownHostPromptsThenAllowOnceFetches(t *testing.T) {
	var fetched bool
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		w.Write([]byte("approved once"))
	})

	var prompt *mcp.ElicitParams
	session := connectWith(t, client, answer("once", &prompt))

	res := callWebFetch(t, session, "https://unknown.example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if !fetched {
		t.Error("approved call performed no fetch")
	}
	if got := resultText(t, res); got != "approved once" {
		t.Errorf("body = %q, want %q", got, "approved once")
	}
	if prompt == nil {
		t.Fatal("no prompt was sent")
	}
	if !strings.Contains(prompt.Message, "unknown.example.com") {
		t.Errorf("prompt %q does not name the requested host", prompt.Message)
	}
}

// The prompt must offer exactly once/always/deny as a flat single-field enum.
func TestPromptOffersExactlyTheThreeChoices(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {})
	var prompt *mcp.ElicitParams
	session := connectWith(t, client, answer("deny", &prompt))

	tryWebFetch(session, "https://unknown.example.com/page")
	if prompt == nil {
		t.Fatal("no prompt was sent")
	}

	raw, err := json.Marshal(prompt.RequestedSchema)
	if err != nil {
		t.Fatalf("marshalling requested schema: %v", err)
	}
	var schema struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parsing requested schema %s: %v", raw, err)
	}

	if schema.Type != "object" {
		t.Errorf("schema type = %q, want %q", schema.Type, "object")
	}
	if len(schema.Properties) != 1 {
		t.Fatalf("schema has %d properties, want exactly 1: %s", len(schema.Properties), raw)
	}
	field, ok := schema.Properties["decision"]
	if !ok {
		t.Fatalf("schema has no \"decision\" property: %s", raw)
	}
	want := []string{"once", "always", "deny"}
	if len(field.Enum) != len(want) {
		t.Fatalf("enum = %v, want %v", field.Enum, want)
	}
	for i, choice := range want {
		if field.Enum[i] != choice {
			t.Errorf("enum[%d] = %q, want %q", i, field.Enum[i], choice)
		}
	}
}

// "always" fetches too. Persisting it is ticket 03.
func TestAllowAlwaysFetches(t *testing.T) {
	var fetched bool
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		w.Write([]byte("approved always"))
	})
	session := connectWith(t, client, answer("always", nil))

	res := callWebFetch(t, session, "https://unknown.example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if !fetched {
		t.Error("approved call performed no fetch")
	}
}

// Fail closed: anything that is not an explicit approval fetches nothing.
func TestNonApprovalOutcomesFetchNothing(t *testing.T) {
	tests := []struct {
		name   string
		answer elicitHandler
	}{
		{"explicit deny", answer("deny", nil)},
		{"declined", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "decline"}, nil
		}},
		{"cancelled", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "cancel"}, nil
		}},
		{"transport failure", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return nil, errors.New("elicitation transport blew up")
		}},
		{"prompt expired", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return nil, context.DeadlineExceeded
		}},
		{"unrecognized choice", answer("yes-please", nil)},
		{"empty choice", answer("", nil)},
		{"accepted with no content", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "accept"}, nil
		}},
		{"accepted with wrong field", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approve": "once"}}, nil
		}},
		{"unrecognized action", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "shrug", Content: map[string]any{"decision": "once"}}, nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fetched bool
			client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
				fetched = true
			})
			session := connectWith(t, client, tt.answer, "example.com")

			res, err := tryWebFetch(session, "https://unknown.example.com/page")
			if !refused(res, err) {
				t.Error("call succeeded, want a refusal")
			}
			if fetched {
				t.Error("refused call performed a fetch, want none")
			}
		})
	}
}

// A prompt is only for unknown hosts. An already-whitelisted host must never
// interrupt a human.
func TestWhitelistedHostIsNotPrompted(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("no prompt needed"))
	})
	var prompted bool
	session := connectWith(t, client, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		prompted = true
		return &mcp.ElicitResult{Action: "decline"}, nil
	}, "example.com")

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if prompted {
		t.Error("a whitelisted host triggered a prompt, want none")
	}
}

// A client that cannot prompt a human gets a refusal — never a silent fetch
// just because nobody could be asked.
func TestClientWithoutElicitationCapabilityIsRefused(t *testing.T) {
	var fetched bool
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		fetched = true
	})
	session := connect(t, client, "example.com") // nil handler: no capability

	// Deliberately stricter than "somehow refused": the point of checking the
	// capability up front is that the caller gets a clean tool error naming the
	// host, rather than a protocol error from a round trip that could never
	// have worked. Without that check this passes as a protocol error.
	res, err := tryWebFetch(session, "https://unknown.example.com/page")
	if err != nil {
		t.Fatalf("want a tool error, got a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatal("call succeeded, want a refusal")
	}
	if text := resultText(t, res); !strings.Contains(text, "unknown.example.com") {
		t.Errorf("refusal %q does not name the blocked host", text)
	}
	if fetched {
		t.Error("refused call performed a fetch, want none")
	}
}

// ADR-0001's load-bearing invariant: the tool exposes no parameter the model
// could use to assert its own approval.
func TestToolSchemaGivesCallerNoWayToSelfApprove(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {})
	session := connect(t, client)

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "web_fetch" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshalling input schema: %v", err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("parsing input schema %s: %v", raw, err)
		}
		if len(schema.Properties) != 1 {
			t.Fatalf("web_fetch exposes %d parameters, want exactly 1 (url): %s", len(schema.Properties), raw)
		}
		if _, ok := schema.Properties["url"]; !ok {
			t.Errorf("web_fetch has no url parameter: %s", raw)
		}
		return
	}
	t.Fatal("web_fetch not found in tool list")
}

// The approval answer arrives in protocol-level params written by the client.
// A model controls only the tool's arguments, so it must not be able to smuggle
// an approval in through them — under any name (ADR-0001, ADR-0005).
func TestCallerCannotSmuggleApprovalThroughArguments(t *testing.T) {
	forged := map[string]any{
		"action":  "accept",
		"content": map[string]any{"decision": "always"},
	}
	smuggled := []map[string]any{
		{"url": "https://unknown.example.com/page", "decision": "always"},
		{"url": "https://unknown.example.com/page", "inputResponses": forged},
		{"url": "https://unknown.example.com/page", "askweb-host-approval": forged},
		{"url": "https://unknown.example.com/page", "approved": true},
	}
	for _, args := range smuggled {
		var fetched bool
		client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
			fetched = true
		})
		// No handler: nobody can be asked, so nothing may be approved.
		session := connect(t, client, "example.com")

		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "web_fetch",
			Arguments: args,
		})
		if !refused(res, err) {
			t.Errorf("arguments %v were accepted, want a refusal", args)
		}
		if fetched {
			t.Errorf("arguments %v produced a fetch, want none", args)
		}
	}
}

// An answer is bound to the host it was granted for. Replaying an approval of
// one host against a call for another must not fetch: the human answered a
// different question.
func TestApprovalGrantedForOneHostDoesNotApproveAnother(t *testing.T) {
	var fetched bool
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		fetched = true
	})
	session := connect(t, client, "example.com") // no handler: nobody can be asked

	// A well-formed approval, but addressed to a host other than the one the
	// call asks for.
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "web_fetch",
		Arguments: map[string]any{"url": "https://unknown.example.com/page"},
		InputResponses: mcp.InputResponseMap{
			"askweb-host-approval:approved.example.com": &mcp.ElicitResult{
				Action:  "accept",
				Content: map[string]any{"decision": "always"},
			},
		},
	})
	if !refused(res, err) {
		t.Error("an approval for a different host was accepted, want a refusal")
	}
	if fetched {
		t.Error("an approval for a different host produced a fetch, want none")
	}
}

// The matching approval, by contrast, does fetch — otherwise the test above
// would pass even if approvals never worked at all.
func TestApprovalGrantedForTheRequestedHostFetches(t *testing.T) {
	var fetched bool
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		w.Write([]byte("approved for this host"))
	})
	session := connect(t, client, "example.com")

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "web_fetch",
		Arguments: map[string]any{"url": "https://unknown.example.com/page"},
		InputResponses: mcp.InputResponseMap{
			"askweb-host-approval:unknown.example.com": &mcp.ElicitResult{
				Action:  "accept",
				Content: map[string]any{"decision": "once"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool returned a protocol error: %v", err)
	}
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if !fetched {
		t.Error("a matching approval performed no fetch")
	}
}

// --- Ticket 03: persisting "always" approvals ---------------------------

// savedHosts reads the whitelist file back off disk.
func savedHosts(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading whitelist: %v", err)
	}
	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parsing whitelist %s: %v", data, err)
	}
	return entries
}

// counting wraps an answer so a test can assert how often a human was asked.
func counting(answer elicitHandler, prompts *int) elicitHandler {
	return func(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		*prompts++
		return answer(ctx, req)
	}
}

func TestAlwaysPersistsTheHostAndStopsPrompting(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("persisted"))
	})
	store, path := loadStore(t, "example.com")

	var prompts int
	session := connectTo(t, client, counting(answer("always", nil), &prompts), store)

	if res := callWebFetch(t, session, "https://unknown.example.com/page"); res.IsError {
		t.Fatalf("first call failed: %s", resultText(t, res))
	}
	if prompts != 1 {
		t.Fatalf("first call asked %d times, want 1", prompts)
	}
	if got := savedHosts(t, path); !slices.Contains(got, "unknown.example.com") {
		t.Errorf("saved whitelist is %v, want it to contain the approved host", got)
	}

	// The whole point: the human is not asked twice.
	if res := callWebFetch(t, session, "https://unknown.example.com/other"); res.IsError {
		t.Fatalf("second call failed: %s", resultText(t, res))
	}
	if prompts != 1 {
		t.Errorf("an approved-always host asked %d times, want to be asked only once", prompts)
	}
}

// The restart: a new store reading the saved file must honour the approval.
func TestAlwaysApprovalSurvivesRestart(t *testing.T) {
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("still approved"))
	})
	store, path := loadStore(t, "example.com")

	session := connectTo(t, client, answer("always", nil), store)
	if res := callWebFetch(t, session, "https://unknown.example.com/page"); res.IsError {
		t.Fatalf("approving call failed: %s", resultText(t, res))
	}

	// Everything below is a fresh process: a new store, server, and session,
	// sharing only the file on disk.
	restarted, err := whitelist.Load(path)
	if err != nil {
		t.Fatalf("reloading whitelist: %v", err)
	}
	var prompts int
	revived := connectTo(t, client, counting(answer("deny", nil), &prompts), restarted)

	if res := callWebFetch(t, revived, "https://unknown.example.com/page"); res.IsError {
		t.Fatalf("call after restart failed: %s", resultText(t, res))
	}
	if prompts != 0 {
		t.Errorf("a host approved before the restart asked %d times after it, want 0", prompts)
	}
}

// Only "always" writes. Every other outcome leaves the file exactly as it was.
func TestOnlyAlwaysChangesTheSavedWhitelist(t *testing.T) {
	tests := []struct {
		name   string
		answer elicitHandler
	}{
		{"once", answer("once", nil)},
		{"deny", answer("deny", nil)},
		{"declined", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "decline"}, nil
		}},
		{"cancelled", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "cancel"}, nil
		}},
		{"transport failure", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return nil, errors.New("elicitation transport blew up")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {})
			store, path := loadStore(t, "example.com")
			before := savedHosts(t, path)

			session := connectTo(t, client, tt.answer, store)
			tryWebFetch(session, "https://unknown.example.com/page")

			if after := savedHosts(t, path); !slices.Equal(before, after) {
				t.Errorf("saved whitelist changed from %v to %v", before, after)
			}
		})
	}
}

// A save failure costs the persistence, not the call: the human did approve it.
func TestApprovedCallSucceedsWhenTheWhitelistCannotBeSaved(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	var fetched bool
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		w.Write([]byte("approved but unsaved"))
	})

	// A path under a directory that does not exist: readable as empty, unwritable.
	path := filepath.Join(t.TempDir(), "no-such-dir", "whitelist.json")
	store, err := whitelist.Load(path)
	if err != nil {
		t.Fatalf("loading whitelist: %v", err)
	}

	var prompts int
	session := connectTo(t, client, counting(answer("always", nil), &prompts), store)

	res := callWebFetch(t, session, "https://unknown.example.com/page")
	if res.IsError {
		t.Fatalf("approved call failed despite the human approving it: %s", resultText(t, res))
	}
	if !fetched {
		t.Error("approved call performed no fetch")
	}
	if !strings.Contains(logged.String(), "unknown.example.com") {
		t.Errorf("save failure was not logged; log holds %q", logged.String())
	}

	// Not persistently allowed: the next call must ask again.
	if res := callWebFetch(t, session, "https://unknown.example.com/other"); res.IsError {
		t.Fatalf("second call failed: %s", resultText(t, res))
	}
	if prompts != 2 {
		t.Errorf("asked %d times, want 2 — an unsaved approval must not be remembered", prompts)
	}
}

// --- Ticket 04: re-validating redirect targets --------------------------

// byHost stands one local origin in for several hostnames. Every connection
// lands on the same server, so the Host header is what tells them apart —
// which is what lets a redirect chain across hosts stay in-process. An
// unlisted host is a test failure: the chain reached somewhere it should not.
func byHost(t *testing.T, handlers map[string]http.HandlerFunc) *http.Client {
	t.Helper()
	return newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		handler, ok := handlers[host]
		if !ok {
			t.Errorf("unexpected request for host %q", r.Host)
			http.Error(w, "no route", http.StatusNotFound)
			return
		}
		handler(w, r)
	})
}

// redirectTo answers with a 302 pointing at url.
func redirectTo(url string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, url, http.StatusFound)
	}
}

func serves(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(text))
	}
}

func TestRedirectToWhitelistedHostIsFollowed(t *testing.T) {
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com":       redirectTo("https://other.example.com/moved"),
		"other.example.com": serves("body from the redirect target"),
	})
	session := connect(t, client, "example.com", "other.example.com")

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != "body from the redirect target" {
		t.Errorf("body = %q, want the redirect target's body", got)
	}
}

// The gap ticket 04 closes: a whitelisted host redirecting somewhere unknown
// must put the target to a human, and must not be requested before they agree.
func TestRedirectToUnknownHostPromptsBeforeBeingFollowed(t *testing.T) {
	var reached bool
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com": redirectTo("https://unknown.example.com/moved"),
		"unknown.example.com": func(w http.ResponseWriter, r *http.Request) {
			reached = true
			w.Write([]byte("body from the unapproved host"))
		},
	})

	var prompt *mcp.ElicitParams
	session := connectWith(t, client, answer("once", &prompt), "example.com")

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if prompt == nil {
		t.Fatal("the redirect target was followed without asking anyone")
	}
	if !strings.Contains(prompt.Message, "unknown.example.com") {
		t.Errorf("prompt %q does not name the redirect target", prompt.Message)
	}
	if !reached {
		t.Error("the approved redirect target was never fetched")
	}
	if got := resultText(t, res); got != "body from the unapproved host" {
		t.Errorf("body = %q, want the approved target's body", got)
	}
}

// "always" on a redirect must keep the host that was actually asked about —
// the target — and must not widen the whitelist to anything else.
func TestAlwaysOnARedirectPersistsTheRedirectTarget(t *testing.T) {
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com":         redirectTo("https://unknown.example.com/moved"),
		"unknown.example.com": serves("persisted target"),
	})
	store, path := loadStore(t, "example.com")
	session := connectTo(t, client, answer("always", nil), store)

	if res := callWebFetch(t, session, "https://example.com/page"); res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}

	want := []string{"example.com", "unknown.example.com"}
	if got := savedHosts(t, path); !slices.Equal(got, want) {
		t.Errorf("saved whitelist = %v, want %v", got, want)
	}
}

// Refusing a hop refuses the whole request: no body, and above all no request
// to the host that was turned down.
func TestRefusedRedirectFetchesNothingFromTheUnapprovedHost(t *testing.T) {
	tests := []struct {
		name   string
		answer elicitHandler
	}{
		{"deny", answer("deny", nil)},
		{"declined", func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "decline"}, nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			client := byHost(t, map[string]http.HandlerFunc{
				"example.com": redirectTo("https://unknown.example.com/moved"),
				"unknown.example.com": func(w http.ResponseWriter, r *http.Request) {
					reached = true
					w.Write([]byte("secret from the unapproved host"))
				},
			})
			session := connectWith(t, client, tt.answer, "example.com")

			res, err := tryWebFetch(session, "https://example.com/page")
			if !refused(res, err) {
				t.Error("a refused redirect succeeded, want a refusal")
			}
			if reached {
				t.Error("the refused host was fetched anyway")
			}
			if err == nil && strings.Contains(resultText(t, res), "secret") {
				t.Errorf("result %q carries content from the refused host", resultText(t, res))
			}
		})
	}
}

// The check is per hop, not per call: an unknown host three hops down the
// chain is caught exactly like one named in the original URL.
func TestEveryHopInAChainIsChecked(t *testing.T) {
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com":         redirectTo("https://one.example.com/a"),
		"one.example.com":     redirectTo("https://two.example.com/b"),
		"two.example.com":     redirectTo("https://unknown.example.com/c"),
		"unknown.example.com": serves("end of the chain"),
	})
	var prompt *mcp.ElicitParams
	session := connectWith(t, client, answer("once", &prompt),
		"example.com", "one.example.com", "two.example.com")

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if prompt == nil {
		t.Fatal("the last hop was followed without asking anyone")
	}
	if !strings.Contains(prompt.Message, "unknown.example.com") {
		t.Errorf("prompt %q does not name the unknown hop", prompt.Message)
	}
	if got := resultText(t, res); got != "end of the chain" {
		t.Errorf("body = %q, want the chain's final body", got)
	}
}

// An unknown hop in the middle is gated before it is requested, even though a
// whitelisted host lies beyond it.
func TestUnknownMiddleHopIsGated(t *testing.T) {
	var reached bool
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com": redirectTo("https://unknown.example.com/middle"),
		"unknown.example.com": func(w http.ResponseWriter, r *http.Request) {
			reached = true
			http.Redirect(w, r, "https://last.example.com/end", http.StatusMovedPermanently)
		},
		"last.example.com": serves("past the middle"),
	})
	session := connect(t, client, "example.com", "last.example.com") // nobody can be asked

	res, err := tryWebFetch(session, "https://example.com/page")
	if !refused(res, err) {
		t.Error("an unknown middle hop was followed, want a refusal")
	}
	if reached {
		t.Error("the unapproved middle hop was fetched")
	}
}

// Fail closed on a redirect too: no prompt possible means the hop is refused,
// never followed on the grounds that nobody could be asked.
func TestUnknownRedirectIsRefusedWhenNobodyCanBePrompted(t *testing.T) {
	var reached bool
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com": redirectTo("https://unknown.example.com/moved"),
		"unknown.example.com": func(w http.ResponseWriter, r *http.Request) {
			reached = true
		},
	})
	session := connect(t, client, "example.com") // nil handler: no capability

	res, err := tryWebFetch(session, "https://example.com/page")
	if err != nil {
		t.Fatalf("want a tool error, got a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatal("call succeeded, want a refusal")
	}
	if text := resultText(t, res); !strings.Contains(text, "unknown.example.com") {
		t.Errorf("refusal %q does not name the blocked redirect target", text)
	}
	if reached {
		t.Error("the refused host was fetched anyway")
	}
}

// A redirect loop between whitelisted hosts terminates rather than spinning.
func TestRedirectChainIsBounded(t *testing.T) {
	// The loop stops itself well past the bound, so a missing bound fails the
	// test outright instead of hanging it.
	const giveUp = 25
	var requests int
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com": func(w http.ResponseWriter, r *http.Request) {
			requests++
			if requests > giveUp {
				w.Write([]byte("the chain was never cut short"))
				return
			}
			http.Redirect(w, r, "https://example.com/again", http.StatusFound)
		},
	})
	session := connect(t, client, "example.com")

	res, err := tryWebFetch(session, "https://example.com/page")
	if !refused(res, err) {
		t.Error("an endless redirect loop returned a result, want an error")
	}
	if requests > 11 {
		t.Errorf("followed %d requests, want a bound of at most 11", requests)
	}
	if requests < 2 {
		t.Errorf("followed %d requests, want redirects to be followed at all", requests)
	}
}

// Two unknown hops, each allowed only for this call. The client replaces the
// answers it sends on every retry, so the second question has to repeat the
// first one — otherwise hop one's approval would be gone by the time hop two
// was answered, and the chain could never complete.
func TestChainOfUnknownHostsCompletesWithOnce(t *testing.T) {
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com":        redirectTo("https://first.example.com/a"),
		"first.example.com":  redirectTo("https://second.example.com/b"),
		"second.example.com": serves("both approved once"),
	})
	// The client fulfils every question in one round in parallel, so the tally
	// is written from several goroutines at once.
	var mu sync.Mutex
	asked := map[string]int{}
	session := connectWith(t, client, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		mu.Lock()
		for _, host := range []string{"first.example.com", "second.example.com"} {
			if strings.Contains(req.Params.Message, host) {
				asked[host]++
			}
		}
		mu.Unlock()
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "once"}}, nil
	}, "example.com")

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != "both approved once" {
		t.Errorf("body = %q, want the chain's final body", got)
	}
	// Hop one is asked about twice: once on its own, then again beside hop two.
	// That repetition is the price of the approval surviving to the last round.
	mu.Lock()
	defer mu.Unlock()
	if asked["first.example.com"] != 2 || asked["second.example.com"] != 1 {
		t.Errorf("asked %v, want first.example.com twice and second.example.com once", asked)
	}
}

// Nothing is recorded when the whitelist cannot be written, so an "always" is
// no more durable than a "once" — and a later hop has to repeat it just the
// same, or the chain stalls.
func TestChainCompletesWhenAnAlwaysApprovalCannotBeSaved(t *testing.T) {
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	client := byHost(t, map[string]http.HandlerFunc{
		"example.com":        redirectTo("https://first.example.com/a"),
		"first.example.com":  redirectTo("https://second.example.com/b"),
		"second.example.com": serves("approved but unsaved"),
	})
	// A path under a directory that does not exist: readable as empty, unwritable.
	store, err := whitelist.Load(filepath.Join(t.TempDir(), "no-such-dir", "whitelist.json"))
	if err != nil {
		t.Fatalf("loading whitelist: %v", err)
	}
	if err := store.Add("example.com"); err == nil {
		t.Fatal("the store saved to an unwritable path, so this test proves nothing")
	}

	session := connectTo(t, client, answer("always", nil), store)

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != "approved but unsaved" {
		t.Errorf("body = %q, want the chain's final body", got)
	}
}

// The same chain does complete when the first hop is kept with "always": the
// approval is on disk by the retry, so only one question is outstanding.
func TestChainOfUnknownHostsCompletesWithAlways(t *testing.T) {
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com":        redirectTo("https://first.example.com/a"),
		"first.example.com":  redirectTo("https://second.example.com/b"),
		"second.example.com": serves("both approved"),
	})
	store, path := loadStore(t, "example.com")
	session := connectTo(t, client, answer("always", nil), store)

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != "both approved" {
		t.Errorf("body = %q, want the chain's final body", got)
	}
	want := []string{"example.com", "first.example.com", "second.example.com"}
	if got := savedHosts(t, path); !slices.Equal(got, want) {
		t.Errorf("saved whitelist = %v, want %v", got, want)
	}
}

// Every redirect status the Go client would itself have followed is followed
// here too, so gating changes which host is reached, not which responses count
// as redirects.
func TestEveryRedirectStatusIsFollowed(t *testing.T) {
	for _, status := range []int{
		http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := byHost(t, map[string]http.HandlerFunc{
				"example.com": func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "https://other.example.com/moved", status)
				},
				"other.example.com": serves("followed"),
			})
			session := connect(t, client, "example.com", "other.example.com")

			res := callWebFetch(t, session, "https://example.com/page")
			if res.IsError {
				t.Fatalf("call failed: %s", resultText(t, res))
			}
			if got := resultText(t, res); got != "followed" {
				t.Errorf("body = %q, want the redirect target's body", got)
			}
		})
	}
}

// A relative Location resolves against the URL it came from, so a same-host
// redirect stays on that host instead of failing to parse.
func TestRelativeRedirectStaysOnTheSameHost(t *testing.T) {
	client := byHost(t, map[string]http.HandlerFunc{
		"example.com": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/elsewhere" {
				w.Write([]byte("arrived via a relative hop"))
				return
			}
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		},
	})
	session := connect(t, client, "example.com")

	res := callWebFetch(t, session, "https://example.com/page")
	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != "arrived via a relative hop" {
		t.Errorf("body = %q, want the relative target's body", got)
	}
}

// A redirect naming no usable target is not a redirect. It falls through to the
// status check and fails there, rather than being followed to nowhere.
func TestRedirectWithoutAUsableLocationIsNotFollowed(t *testing.T) {
	for _, location := range []string{"", ":://not a url"} {
		client := byHost(t, map[string]http.HandlerFunc{
			"example.com": func(w http.ResponseWriter, r *http.Request) {
				if location != "" {
					w.Header().Set("Location", location)
				}
				w.WriteHeader(http.StatusFound)
			},
		})
		session := connect(t, client, "example.com")

		res, err := tryWebFetch(session, "https://example.com/page")
		if !refused(res, err) {
			t.Errorf("Location %q produced a result, want an error", location)
		}
	}
}

// The gate reads the hop's host, not its scheme, so a cross-scheme redirect is
// checked like any other — and a scheme that is not http or https is refused
// outright by canonicalization.
func TestRedirectScheme(t *testing.T) {
	tests := []struct {
		name     string
		location string
		want     string
	}{
		{"to http", "http://unknown.example.com/moved", "unknown.example.com"},
		{"to a non-web scheme", "file:///etc/passwd", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			client := byHost(t, map[string]http.HandlerFunc{
				"example.com": redirectTo(tt.location),
				"unknown.example.com": func(w http.ResponseWriter, r *http.Request) {
					reached = true
				},
			})
			var prompt *mcp.ElicitParams
			session := connectWith(t, client, answer("deny", &prompt), "example.com")

			res, err := tryWebFetch(session, "https://example.com/page")
			if !refused(res, err) {
				t.Error("call succeeded, want a refusal")
			}
			if reached {
				t.Error("the redirect target was fetched")
			}
			switch {
			case tt.want == "" && prompt != nil:
				t.Errorf("a %s target was put to a human as %q", tt.name, prompt.Message)
			case tt.want != "" && prompt == nil:
				t.Fatalf("the %s target was never put to a human", tt.name)
			case tt.want != "" && !strings.Contains(prompt.Message, tt.want):
				t.Errorf("prompt %q does not name %q", prompt.Message, tt.want)
			}
		})
	}
}

// An answer about the wrong host is not an answer about this one. It is asked
// about properly rather than taken as a refusal — and never taken as consent.
func TestApprovalForAnotherHostIsAskedAboutAgain(t *testing.T) {
	var fetched bool
	client := newOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		w.Write([]byte("fetched"))
	})
	var prompt *mcp.ElicitParams
	session := connectWith(t, client, answer("deny", &prompt), "example.com")

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "web_fetch",
		Arguments: map[string]any{"url": "https://unknown.example.com/page"},
		InputResponses: mcp.InputResponseMap{
			"askweb-host-approval:approved.example.com": &mcp.ElicitResult{
				Action:  "accept",
				Content: map[string]any{"decision": "always"},
			},
		},
	})
	if !refused(res, err) {
		t.Error("an approval for a different host was accepted, want a refusal")
	}
	if fetched {
		t.Error("an approval for a different host produced a fetch, want none")
	}
	if prompt == nil {
		t.Fatal("the requested host was never put to a human")
	}
	if !strings.Contains(prompt.Message, "unknown.example.com") {
		t.Errorf("prompt %q asks about the wrong host", prompt.Message)
	}
}
