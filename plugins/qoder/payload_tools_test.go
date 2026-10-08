package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildInferPayloadAlwaysCarriesAToolsArray pins the upstream contract:
// `tools` is a top-level key that is always present, and an empty array when
// the client sent none — never a missing field (`tools: ask.tools ?? []`,
// gitee `qoder-wasm.ts:316-321`).
func TestBuildInferPayloadAlwaysCarriesAToolsArray(t *testing.T) {
	encoded, errPayload := buildInferPayload(inferAsk{ModelKey: "auto", UserText: "x"})
	if errPayload != nil {
		t.Fatalf("buildInferPayload: %v", errPayload)
	}
	var decoded map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(encoded, &decoded); errUnmarshal != nil {
		t.Fatalf("decode payload: %v", errUnmarshal)
	}
	raw, ok := decoded["tools"]
	if !ok {
		t.Fatal("payload is missing the top-level `tools` key")
	}
	if string(raw) != "[]" {
		t.Fatalf("tools = %s, want an empty array", raw)
	}
}

// TestBuildInferPayloadSendsWrappedTools is the regression guard for the
// reported "qwen3.8-flash 执行任务出现任务调用 xml 泄露" failure: the early
// implementation hardcoded `tools: []`, so models never saw a function schema
// and invented XML tool calls inside the reply text. Tools must reach the
// payload wrapped as `{type:'function', function:{…}}`, with `description` /
// `parameters` omitted when empty (the client's `$Hc(A)` shape).
func TestBuildInferPayloadSendsWrappedTools(t *testing.T) {
	ask := inferAsk{
		ModelKey: "qfmodel", UserText: "u",
		Tools: []inferTool{
			{Type: "function", Function: inferToolFunction{
				Name:       "read_file",
				Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			}},
			{Type: "function", Function: inferToolFunction{Name: "no_schema", Description: "d"}},
		},
	}
	encoded, errPayload := buildInferPayload(ask)
	if errPayload != nil {
		t.Fatalf("buildInferPayload: %v", errPayload)
	}
	var decoded struct {
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	if errUnmarshal := json.Unmarshal(encoded, &decoded); errUnmarshal != nil {
		t.Fatalf("decode tools: %v", errUnmarshal)
	}
	if len(decoded.Tools) != 2 {
		t.Fatalf("tools = %d entries, want 2", len(decoded.Tools))
	}

	var firstFunction map[string]json.RawMessage
	if errFunction := json.Unmarshal(decoded.Tools[0]["function"], &firstFunction); errFunction != nil {
		t.Fatalf("decode first function: %v", errFunction)
	}
	if _, ok := firstFunction["description"]; ok {
		t.Fatalf("an empty description must not be serialized: %s", firstFunction["description"])
	}
	if !strings.Contains(string(firstFunction["parameters"]), `"path"`) {
		t.Fatalf("parameters = %s, want the client schema verbatim", firstFunction["parameters"])
	}
	if string(decoded.Tools[0]["type"]) != `"function"` {
		t.Fatalf("type = %s, want \"function\"", decoded.Tools[0]["type"])
	}

	var secondFunction map[string]json.RawMessage
	if errFunction := json.Unmarshal(decoded.Tools[1]["function"], &secondFunction); errFunction != nil {
		t.Fatalf("decode second function: %v", errFunction)
	}
	if _, ok := secondFunction["parameters"]; ok {
		t.Fatal("missing parameters must not be serialized")
	}
	if string(secondFunction["description"]) != `"d"` {
		t.Fatalf("description = %s, want %q", secondFunction["description"], "d")
	}
}

// TestInferAskFromRequestMapsToolsAndToolMessages covers the full wiring: the
// client's tools arrive in the ask, and assistant `tool_calls` / `tool`
// `tool_call_id` survive into the history. Dropping the assistant side makes
// models re-call the same tool or invent results (gitee `qoder-wasm.ts:127-134`).
func TestInferAskFromRequestMapsToolsAndToolMessages(t *testing.T) {
	payload := `{
		"model":"qfmodel",
		"messages":[
			{"role":"user","content":"read a.txt"},
			{"role":"assistant","content":"","tool_calls":[
				{"id":"call_1","type":"function","index":0,"function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]},
			{"role":"tool","content":"42","tool_call_id":"call_1"}
		],
		"tools":[
			{"type":"function","function":{"name":"read_file","description":"Read a file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}},
			{"type":"web_search","function":{"name":"search"}},
			{"type":"function","function":{"name":"  "}}
		]}`
	request := executorRequestFixture(payload, "qfmodel")
	ask, errAsk := inferAskFromRequest(request, &Credential{AccessToken: "tok"}, DefaultConfig(), productByID(string(RegionGlobal)))
	if errAsk != nil {
		t.Fatalf("inferAskFromRequest: %v", errAsk)
	}

	if len(ask.Tools) != 1 {
		t.Fatalf("tools = %d entries, want only the function tool with a name", len(ask.Tools))
	}
	if ask.Tools[0].Function.Name != "read_file" || ask.Tools[0].Function.Description != "Read a file" {
		t.Fatalf("tool = %#v", ask.Tools[0])
	}
	if !strings.Contains(string(ask.Tools[0].Function.Parameters), `"path"`) {
		t.Fatalf("parameters = %s", ask.Tools[0].Function.Parameters)
	}

	if len(ask.History) != 3 {
		t.Fatalf("history = %d entries, want 3", len(ask.History))
	}
	if len(ask.History[0].ToolCalls) != 0 || ask.History[0].ToolCallID != "" {
		t.Fatalf("the user turn must carry neither tool field: %#v", ask.History[0])
	}
	assistant := ask.History[1]
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls were dropped: %#v", assistant)
	}
	call := assistant.ToolCalls[0]
	if call.ID != "call_1" || call.Type != "function" || call.Index == nil || *call.Index != 0 {
		t.Fatalf("tool call = %#v", call)
	}
	if call.Function.Name != "read_file" || call.Function.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("tool call function = %#v (arguments must stay the raw string)", call.Function)
	}
	if assistant.ToolCallID != "" {
		t.Fatalf("assistant must not carry tool_call_id: %#v", assistant)
	}
	toolTurn := ask.History[2]
	if toolTurn.ToolCallID != "call_1" || len(toolTurn.ToolCalls) != 0 {
		t.Fatalf("tool turn = %#v", toolTurn)
	}
}

// TestInferToolsFromWireFiltering pins the filter rules: non-function types are
// rejected by the encrypted endpoint, so they are never forwarded; empty types
// count as the OpenAI default; nameless entries are dropped.
func TestInferToolsFromWireFiltering(t *testing.T) {
	mapped := inferToolsFromWire([]wireTool{
		{Type: "", Function: wireToolFunction{Name: "a"}},
		{Type: "FUNCTION", Function: wireToolFunction{Name: "b"}},
		{Type: "web_search", Function: wireToolFunction{Name: "c"}},
		{Type: "function", Function: wireToolFunction{Name: "  "}},
	})
	if len(mapped) != 2 {
		t.Fatalf("mapped = %#v, want exactly a and b", mapped)
	}
	if mapped[0].Function.Name != "a" || mapped[1].Function.Name != "b" {
		t.Fatalf("mapped = %#v", mapped)
	}
	for _, tool := range mapped {
		if tool.Type != "function" {
			t.Fatalf("type = %q, want the normalized function type", tool.Type)
		}
	}
}
