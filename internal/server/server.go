// Package server assembles the askweb MCP server and its web_fetch tool.
package server

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MrModest/askweb/internal/hostname"
	"github.com/MrModest/askweb/internal/whitelist"
)

const version = "v0.1.0"

// fetchInput is the web_fetch argument schema.
//
// It carries a URL and nothing else. Adding a parameter that could influence
// whether a host is allowed would let the model approve its own fetches and
// defeat the whitelist entirely — see ADR-0001.
type fetchInput struct {
	URL string `json:"url" jsonschema:"absolute http or https URL to fetch"`
}

// New builds the MCP server. client performs the outbound fetches.
func New(store *whitelist.Store, client *http.Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "askweb", Version: version}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name: "web_fetch",
		Description: "Fetch the contents of a URL. Access is limited to an " +
			"operator-controlled whitelist of hostnames; a URL on any other host is refused.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in fetchInput) (*mcp.CallToolResult, any, error) {
		return fetch(ctx, store, client, in.URL)
	})
	return s
}

func fetch(ctx context.Context, store *whitelist.Store, client *http.Client, rawURL string) (*mcp.CallToolResult, any, error) {
	host, err := hostname.Canonical(rawURL)
	if err != nil {
		return nil, nil, err
	}

	if !store.Allowed(host) {
		// Name the blocked host and nothing else: no whitelist contents, no
		// internal detail.
		return nil, nil, fmt.Errorf("host %q is not on the whitelist, so nothing was fetched", host)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("fetching %s: %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
