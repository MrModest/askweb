package server_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// connect wires a client to a server built over the given whitelist entries,
// using in-memory transports so no MCP traffic touches the network.
func connect(t *testing.T, client *http.Client, entries ...string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	store, err := whitelist.Load(writeWhitelist(t, entries...))
	if err != nil {
		t.Fatalf("loading whitelist: %v", err)
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.New(store, client).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil).
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

			res := callWebFetch(t, session, tt.url)
			if !res.IsError {
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
	if !strings.Contains(text, "whitelist") {
		t.Errorf("refusal %q is not a whitelist refusal", text)
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
