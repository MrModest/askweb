package approval

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const host = "unknown.example.com"

// accepted builds a well-formed approval addressed to host.
func accepted(choice string) mcp.InputResponseMap {
	return mcp.InputResponseMap{
		requestID(host): &mcp.ElicitResult{
			Action:  "accept",
			Content: map[string]any{field: choice},
		},
	}
}

func TestDecideAllowsExplicitApprovals(t *testing.T) {
	tests := []struct {
		choice string
		want   Outcome
	}{
		{choiceOnce, Once},
		{choiceAlways, Always},
	}
	for _, tt := range tests {
		if got := Decide(accepted(tt.choice), host); got != tt.want {
			t.Errorf("Decide(%q) = %v, want %v", tt.choice, got, tt.want)
		}
	}
}

// Fail closed. The client SDK rejects some of these against the requested
// schema before they ever reach Decide, but the guarantee is Decide's own: it
// must never read permission into an answer that does not explicitly grant it.
func TestDecideDeniesEverythingElse(t *testing.T) {
	tests := []struct {
		name      string
		responses mcp.InputResponseMap
	}{
		{"explicit deny", accepted(choiceDeny)},
		{"unrecognized choice", accepted("yes-please")},
		{"empty choice", accepted("")},
		{"nil responses", nil},
		{"empty responses", mcp.InputResponseMap{}},
		{"declined", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{Action: "decline"},
		}},
		{"cancelled", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{Action: "cancel"},
		}},
		{"unrecognized action", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{
				Action:  "shrug",
				Content: map[string]any{field: choiceAlways},
			},
		}},
		{"choice of the wrong type", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{
				Action:  "accept",
				Content: map[string]any{field: true},
			},
		}},
		{"wrong response type", mcp.InputResponseMap{
			requestID(host): &mcp.ListRootsResult{},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.responses, host); got != Deny {
				t.Errorf("Decide = %v, want Deny", got)
			}
		})
	}
}

// A client that can only carry the action back still gets its human's approval
// honoured — as a "once", the least the accept could have meant. Nothing here
// may ever come back Always: a whitelist entry needs the word (ADR-0008).
func TestDecideReadsAnAcceptWithoutAChoiceAsOnce(t *testing.T) {
	tests := []struct {
		name      string
		responses mcp.InputResponseMap
	}{
		{"no content at all", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{Action: "accept"},
		}},
		{"empty content", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{
				Action:  "accept",
				Content: map[string]any{},
			},
		}},
		{"content without the choice", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{
				Action:  "accept",
				Content: map[string]any{"approve": choiceAlways},
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.responses, host); got != Once {
				t.Errorf("Decide = %v, want Once", got)
			}
		})
	}
}

// The prompt asks for the choice but must not require it, or the SDK rejects a
// consent-only client's answer against this very schema and the call dies
// before Decide can read the human's accept (ADR-0008).
func TestRequestDoesNotRequireTheChoice(t *testing.T) {
	built := Request(host)[requestID(host)]
	request, ok := built.(*mcp.ElicitParams)
	if !ok {
		t.Fatalf("Request built a %T, want an elicitation prompt", built)
	}
	schema, ok := request.RequestedSchema.(*jsonschema.Schema)
	if !ok {
		t.Fatalf("prompt carries a %T schema, want a *jsonschema.Schema", request.RequestedSchema)
	}
	if len(schema.Required) != 0 {
		t.Errorf("requested schema requires %v, want nothing required", schema.Required)
	}
	if _, ok := schema.Properties[field]; !ok {
		t.Errorf("requested schema does not offer the %q choice at all", field)
	}
}

// An answer is bound to the host it was granted for. Approving one host must
// never carry over to another.
func TestDecideRejectsAnApprovalAddressedToAnotherHost(t *testing.T) {
	if got := Decide(accepted(choiceAlways), "other.example.com"); got != Deny {
		t.Errorf("Decide for a different host = %v, want Deny", got)
	}
}

func TestRequestAsksAboutTheGivenHost(t *testing.T) {
	requests := Request(host)
	if len(requests) != 1 {
		t.Fatalf("Request built %d input requests, want exactly 1", len(requests))
	}
	if _, ok := requests[requestID(host)]; !ok {
		t.Errorf("Request is not keyed by the host it asks about: %v", requests)
	}
}

// Answered is about presence, not permission: a refusal is still an answer,
// and an answer about another host is not one about this host.
func TestAnsweredDistinguishesUnaskedFromRefused(t *testing.T) {
	tests := []struct {
		name      string
		responses mcp.InputResponseMap
		want      bool
	}{
		{"nothing sent", nil, false},
		{"nothing about this host", mcp.InputResponseMap{}, false},
		{"answer about another host", mcp.InputResponseMap{
			requestID("other.example.com"): &mcp.ElicitResult{Action: "accept"},
		}, false},
		{"approved", accepted(choiceOnce), true},
		{"refused", accepted(choiceDeny), true},
		{"declined", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{Action: "decline"},
		}, true},
	}
	for _, tt := range tests {
		if got := Answered(tt.responses, host); got != tt.want {
			t.Errorf("Answered(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
