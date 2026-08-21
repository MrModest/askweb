// Package server assembles the askweb MCP server and its web_fetch tool.
package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MrModest/askweb/internal/approval"
	"github.com/MrModest/askweb/internal/hostname"
	"github.com/MrModest/askweb/internal/whitelist"
)

const version = "v0.1.0"

// maxRedirects bounds a redirect chain, so a server that redirects to itself
// cannot spin forever. It matches the Go client's own default.
const maxRedirects = 10

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
	// Redirects are followed one hop at a time so that every target is checked
	// against the whitelist, which means the client must hand each redirect
	// back instead of chasing it. Copying leaves the caller's client alone.
	gated := *client
	gated.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	s := mcp.NewServer(&mcp.Implementation{Name: "askweb", Version: version}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name: "web_fetch",
		Description: "Fetch the contents of a URL. Access is limited to an " +
			"operator-controlled whitelist of hostnames; reaching any other host " +
			"requires a human to approve it, and is refused without that approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in fetchInput) (*mcp.CallToolResult, any, error) {
		return fetch(ctx, store, &gated, req, in.URL)
	})
	return s
}

func fetch(ctx context.Context, store *whitelist.Store, client *http.Client, req *mcp.CallToolRequest, rawURL string) (*mcp.CallToolResult, any, error) {
	target := rawURL

	// Hosts allowed so far by an answer that will not outlive this round trip.
	// A client sends back answers only to the questions it was last asked, so
	// if a later hop has to ask about a new host, these have to be asked about
	// again alongside it or their approvals are gone by the retry.
	var pending []string

	for hop := 0; ; hop++ {
		host, err := hostname.Canonical(target)
		if err != nil {
			return nil, nil, err
		}

		prompt, transient, err := allow(store, req, host, pending)
		if err != nil {
			return nil, nil, err
		}
		if prompt != nil {
			return prompt, nil, nil
		}
		if transient {
			pending = append(pending, host)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, nil, err
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, err
		}

		// A redirect is not content: nothing is read from it, and the hop it
		// names goes back through allow before it is requested.
		if location, ok := redirectTarget(resp); ok {
			resp.Body.Close()
			if hop == maxRedirects {
				return nil, nil, fmt.Errorf("fetching %s: stopped after %d redirects", rawURL, maxRedirects)
			}
			target = location
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, nil, fmt.Errorf("fetching %s: %s", target, resp.Status)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
}

// allow decides whether host may be reached. Every hop of a redirect chain goes
// through it, not just the URL that was asked for.
//
// A whitelisted host is allowed without interrupting anyone. An unknown one
// needs a human, over two round trips: allow returns a prompt to send back, and
// the client retries the call with the answer attached.
//
// It returns a prompt to return to the client when a human has to be asked, an
// error when the host is refused, and otherwise nil, nil — meaning go ahead.
// transient reports that the host was allowed by an answer that will not
// outlive this round trip, so a later hop must ask about it again. pending is
// every earlier hop in that position.
func allow(store *whitelist.Store, req *mcp.CallToolRequest, host string, pending []string) (*mcp.CallToolResult, bool, error) {
	if store.Allowed(host) {
		return nil, false, nil
	}

	// Name the blocked host and nothing else: no whitelist contents, no
	// internal detail.
	denied := fmt.Errorf("access to host %q was not approved, so nothing was fetched", host)

	switch {
	case approval.Answered(req.Params.InputResponses, host):
		switch approval.Decide(req.Params.InputResponses, host) {
		case approval.Once:
			// Allowed for this call only; nothing is recorded, so the answer
			// lasts no longer than the round trip that carried it.
			return nil, true, nil
		case approval.Always:
			// A human chose to keep this host. If it cannot be saved, the call
			// still proceeds — they did approve it — but the host is not
			// remembered, which leaves the answer exactly as durable as a
			// "once": good for this call and gone afterwards.
			if err := store.Add(host); err != nil {
				log.Printf("not persisting approval for %q: %v", host, err)
				return nil, true, nil
			}
			return nil, false, nil
		default:
			// Deny, and anything a future Outcome might add. Only the two
			// explicit approvals above reach the fetch (ADR-0001).
			return nil, false, denied
		}
	case !approval.ClientCanPrompt(req.Session):
		// Nobody can be asked, so nobody approved. Never fetch on the grounds
		// that the question could not be put.
		return nil, false, denied
	default:
		return &mcp.CallToolResult{InputRequests: approval.Request(append(pending, host)...)}, false, nil
	}
}

// redirectTarget returns the absolute URL a redirect response points at.
//
// Only the statuses the Go client would itself have followed count, so gating
// redirects changes which host is reached, never which responses are treated as
// redirects at all.
func redirectTarget(resp *http.Response) (string, bool) {
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return "", false
	}
	// Location resolves a relative target against the URL it came from, so a
	// same-host redirect stays on that host rather than failing to parse.
	location, err := resp.Location()
	if err != nil {
		return "", false
	}
	return location.String(), true
}
