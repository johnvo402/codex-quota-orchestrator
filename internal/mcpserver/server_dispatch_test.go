package mcpserver

import (
	"encoding/json"
	"testing"
)

func TestIsTaskCompleteRequest(t *testing.T) {
	params, err := json.Marshal(callParams{Name: "task_complete"})
	if err != nil {
		t.Fatal(err)
	}
	if !isTaskCompleteRequest(request{Method: "tools/call", Params: params}) {
		t.Fatal("task_complete tools/call must trigger an immediate delivery drain")
	}
}

func TestIsTaskCompleteRequestRejectsOtherRequests(t *testing.T) {
	for _, tc := range []request{
		{Method: "ping"},
		{Method: "tools/call", Params: json.RawMessage(`{"name":"quota_check"}`)},
		{Method: "tools/call", Params: json.RawMessage(`not-json`)},
	} {
		if isTaskCompleteRequest(tc) {
			t.Fatalf("unexpected delivery drain for request: %+v", tc)
		}
	}
}
