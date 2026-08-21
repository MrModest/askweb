// Package approval asks a human whether an unknown host may be fetched.
//
// The prompt is carried as a multi-round-trip input request (SEP-2322): the
// tool handler returns a request for input, the client puts it to a human, and
// the call is retried with the answer attached. Protocol version 2026-07-28
// forbids a server from initiating elicitation while it is serving a request,
// so this — not ServerSession.Elicit — is the portable shape. The SDK bridges
// clients on older protocol versions automatically.
//
// The model requesting the fetch cannot see, supply, or influence the answer:
// the prompt is answered by the client's human, and web_fetch exposes no
// parameter through which a caller could assert its own approval (ADR-0001).
package approval

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Outcome is what the human decided.
type Outcome int

const (
	// Deny is the zero value on purpose: every path that is not an explicit
	// approval — a decline, a cancel, a timeout, a transport failure, an answer
	// matching no choice — must land here.
	Deny Outcome = iota
	// Once allows this call and nothing beyond it.
	Once
	// Always allows this call and marks the host for persistent approval.
	Always
)

// requestID keys the input request by the host it asks about, so an answer is
// structurally bound to the question. A reply granted for one host is simply
// not found when the retry is about a different one, and the default denial
// applies — a human who approved A can never have that answer count for B.
func requestID(host string) string {
	return "askweb-host-approval:" + host
}

// field is the single property the prompt asks for.
const field = "decision"

const (
	choiceOnce   = "once"
	choiceAlways = "always"
	choiceDeny   = "deny"
)

// Request builds the prompt asking whether hosts may be fetched. Returning it
// from a tool handler suspends the call until a human answers.
//
// Asking about several hosts at once is how a chain of redirects keeps the
// approvals it has already been given. A client answers the questions in the
// map it was last handed and sends those answers back together, replacing what
// it sent before — so a host left out of a later prompt is a host whose
// approval is gone by the time the call is retried.
func Request(hosts ...string) mcp.InputRequestMap {
	requests := make(mcp.InputRequestMap, len(hosts))
	for _, host := range hosts {
		requests[requestID(host)] = &mcp.ElicitParams{
			Message: fmt.Sprintf("Allow fetching from %s? It is not on the whitelist.", host),
			RequestedSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					field: {
						Type:        "string",
						Description: fmt.Sprintf("whether to allow fetching from %s", host),
						Enum:        []any{choiceOnce, choiceAlways, choiceDeny},
					},
				},
				Required: []string{field},
			},
		}
	}
	return requests
}

// Decide reads the human's answer about host out of a retried call's input
// responses.
//
// Anything other than an explicit approval of this host is a Deny, so a
// missing, malformed, unrecognized, or differently-addressed answer can never
// be mistaken for permission.
func Decide(responses mcp.InputResponseMap, host string) Outcome {
	result, ok := responses[requestID(host)].(*mcp.ElicitResult)
	if !ok || result.Action != "accept" {
		return Deny
	}
	switch result.Content[field] {
	case choiceOnce:
		return Once
	case choiceAlways:
		return Always
	default:
		return Deny
	}
}

// ClientCanPrompt reports whether the connected client declared that it can put
// a prompt to a human. If it cannot, an unknown host must be denied rather than
// fetched silently.
func ClientCanPrompt(session *mcp.ServerSession) bool {
	// InitializeParams is nil until the client initializes, and the SDK's
	// checkInitialized guard is currently disabled — so a tools/call arriving
	// before initialize reaches this code. Treat that as "cannot prompt".
	params := session.InitializeParams()
	return params != nil && params.Capabilities != nil && params.Capabilities.Elicitation != nil
}

// Answered reports whether responses carry an answer about host — any answer,
// including a refusal.
//
// It separates "this host has not been put to a human yet" from "a human said
// no", which a chain of redirects makes distinct: a retry carrying approval for
// one hop says nothing at all about the next one, and that hop still has to be
// asked rather than assumed refused.
func Answered(responses mcp.InputResponseMap, host string) bool {
	_, ok := responses[requestID(host)]
	return ok
}
