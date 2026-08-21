package approval

import (
	"testing"

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
		{"accepted with no content", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{Action: "accept"},
		}},
		{"accepted with wrong field", mcp.InputResponseMap{
			requestID(host): &mcp.ElicitResult{
				Action:  "accept",
				Content: map[string]any{"approve": choiceAlways},
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
