package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/collegeming/cpa-jethub-plugins/internal/abiboot"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func tailBody(t *testing.T, values []string, finish string) []byte {
	t.Helper()
	var body bytes.Buffer
	for _, value := range values {
		raw, _ := json.Marshal(map[string]any{"response": value})
		fmt.Fprintf(&body, "event: output\ndata: %s\n\n", raw)
	}
	if finish != "" {
		fmt.Fprintf(&body, "event: done\ndata: {\"finish_reason\":%q}\n\n", finish)
	}
	return body.Bytes()
}

func assertTailCompletion(t *testing.T, body []byte, content, finish string) {
	t.Helper()
	completion, err, saw := AggregateSOLOToCompletion(body, "model", "id", 1)
	if err != nil || !saw {
		t.Fatalf("aggregate err=%v saw=%v", err, saw)
	}
	choices, _ := asSlice(completion["choices"])
	choice := mustMap(t, choices[0])
	message := mustMap(t, choice["message"])
	if message["content"] != content || choice["finish_reason"] != finish {
		t.Fatalf("nonstream content=%q finish=%v, want %q/%s", message["content"], choice["finish_reason"], content, finish)
	}
	frames, err, saw := TranslateSOLOStream(body, "model", "id", 1)
	if err != nil || !saw {
		t.Fatalf("stream err=%v saw=%v", err, saw)
	}
	var got strings.Builder
	for _, frame := range frames {
		var chunk openAIChunk
		if err := json.Unmarshal(frame, &chunk); err != nil {
			t.Fatal(err)
		}
		for _, choice := range chunk.Choices {
			if value, ok := choice.Delta["content"].(string); ok {
				got.WriteString(value)
			}
		}
	}
	if got.String() != content || translateFinishReason(t, frames) != finish {
		t.Fatalf("stream content=%q finish=%s, want %q/%s", got.String(), translateFinishReason(t, frames), content, finish)
	}
}

func TestSOLOTextTailGuardAtEveryByteBoundary(t *testing.T) {
	const marker = "[CPA_END_test47]"
	for _, answer := range []string{"CPA_TEXT_COMPLETE_47", "今天适合出门，请记得携带雨伞。", "{\"value\":47}", "```go\nreturn 47\n```"} {
		wire := answer + "\n\n" + marker + " padding padding padding"
		for cut := 0; cut <= len(wire); cut++ {
			// Split JSON string chunks on valid UTF-8 boundaries, as upstream does.
			if cut < len(wire) && wire[cut]&0xc0 == 0x80 {
				continue
			}
			body := tailBody(t, []string{wire[:cut], wire[cut:]}, "stop")
			filtered, err := filterSOLOTextTail(body, marker)
			if err != nil {
				t.Fatal(err)
			}
			assertTailCompletion(t, filtered, answer, "stop")
			if bytes.Contains(filtered, []byte(marker)) || bytes.Contains(filtered, []byte("padding")) {
				t.Fatal("trailer leaked")
			}
		}
	}
}

func TestSOLOTextTailGuardMissingTrailerAndTerminal(t *testing.T) {
	const marker = "[CPA_END_test47]"
	for _, tc := range []struct{ name, text, marker, finish, want string }{
		{"missing_marker", "CPA_TEXT", marker, "stop", "length"},
		{"inline_marker", "Mention " + marker + " inline", marker, "stop", "length"},
		{"text_eof", "partial", "", "", "length"},
		{"upstream_length", "partial", marker, "length", "length"},
		{"content_filter", "partial", marker, "content_filter", "content_filter"},
		{"guard_disabled", "normal", "", "stop", "stop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filtered, err := filterSOLOTextTail(tailBody(t, []string{tc.text}, tc.finish), tc.marker)
			if err != nil {
				t.Fatal(err)
			}
			assertTailCompletion(t, filtered, tc.text, tc.want)
		})
	}
	filteredPartial, partialErr := filterSOLOTextTail(tailBody(t, []string{"complete\n[CPA_END_test4"}, "stop"), marker)
	if partialErr != nil {
		t.Fatal(partialErr)
	}
	assertTailCompletion(t, filteredPartial, "complete", "length")
	// EOF without a final newline must retain the final text fragment.
	filtered, err := filterSOLOTextTail([]byte("event: output\ndata: {\"response\":\"tail\"}"), "")
	if err != nil {
		t.Fatal(err)
	}
	assertTailCompletion(t, filtered, "tail", "length")
}

func TestSOLOTextTailGuardExecutorFiltersOneUpstreamRequest(t *testing.T) {
	const marker = "[CPA_END_executor47]"
	calls := 0
	fakeHost(t, func(method string, request []byte) (any, error) {
		if method != pluginabi.MethodHostHTTPDo {
			t.Fatalf("unexpected callback %s", method)
		}
		calls++
		return traeHTTPResponse(200, string(tailBody(t, []string{"complete\n" + marker + " padding padding"}, "stop"))), nil
	})
	host := abiboot.NewHost(json.RawMessage(`{"host_callback_id":"tail-test"}`))
	response, err := sendChat(host, &Credential{}, &chatCall{URL: "https://example.invalid", TextTailMarker: marker}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	assertTailCompletion(t, response.Body, "complete", "stop")
	if calls != 1 {
		t.Fatalf("upstream requests=%d, must not replay", calls)
	}
}

func TestSOLOTextTailGuardPreservesToolsReasoningUsageAndErrors(t *testing.T) {
	const marker = "[CPA_END_test47]"
	body := fragmentTestBody(t, [][]map[string]any{
		{fragmentTestEntry(0, "call_add", "add", `{"a":`)},
		{fragmentTestEntry(0, "", "", `19,"b":23}`)},
	})
	if _, err := filterSOLOTextTail(body, marker); err == nil {
		t.Fatal("unterminated tool call must not be forwarded")
	}
	body = append(body, []byte("event: output\ndata: {\"reasoning_content\":\"why\",\"multimodal_contents\":[\"vendor_field\"]}\n\nevent: token_usage\ndata: {\"prompt_tokens\":11,\"completion_tokens\":22}\n\nevent: done\ndata: {\"finish_reason\":\"stop\"}\n\n")...)
	filtered, err := filterSOLOTextTail(body, marker)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _ := AggregateSOLO(filtered, "model")
	if aggregate.ReasoningContent != "why" || len(aggregate.ToolCalls) != 1 || aggregate.FinishReason != "tool_calls" || aggregate.Usage["completion_tokens"] != float64(22) {
		t.Fatalf("aggregate=%#v", aggregate)
	}
	name, args, id := toolCallFields(aggregate.ToolCalls[0])
	if name != "add" || id != "call_add" || args != `{"a":19,"b":23}` {
		t.Fatalf("tool=%s/%s/%s", name, id, args)
	}
	if !bytes.Contains(filtered, []byte("vendor_field")) {
		t.Fatal("vendor fields lost")
	}
	frames, streamErr, _ := TranslateSOLOStream(filtered, "model", "id", 1)
	if streamErr != nil || translateFinishReason(t, frames) != "tool_calls" {
		t.Fatal("stream tool round broken")
	}
	errorBody := []byte("event: error\ndata: {\"code\":4008,\"message\":\"quota\"}\n\n")
	filtered, err = filterSOLOTextTail(errorBody, marker)
	if err != nil || !bytes.Equal(filtered, errorBody) {
		t.Fatal("vendor error mapping changed")
	}
	empty := []byte(": heartbeat\n\n")
	filtered, err = filterSOLOTextTail(empty, marker)
	if err != nil || !bytes.Equal(filtered, empty) {
		t.Fatal("empty-upstream retry signal changed")
	}
}

func TestSOLOTextTailGuardRequestIntegration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DiscoverModels = false
	cfg.MaxMode = false
	for _, tc := range []struct {
		model   string
		enabled bool
	}{
		{"deepseek-v4.1-flash", true}, {"glm-5.2", true}, {"deepseek-v4.1-flash", false},
	} {
		cfg.TextTailGuard = tc.enabled
		raw := []byte(`{"messages":[{"role":"user","content":"answer"}],"max_tokens":4096}`)
		call, err := prepareChatCall(nil, pluginapi.ExecutorRequest{Model: tc.model, Payload: raw}, &Credential{}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		want := tc.enabled && tc.model == "deepseek-v4.1-flash"
		if (call.TextTailMarker != "") != want {
			t.Fatalf("model=%s marker=%q", tc.model, call.TextTailMarker)
		}
		if want && !bytes.Contains(call.Body, []byte(call.TextTailMarker)) {
			t.Fatal("marker missing from actual request")
		}
	}
	if !ConfigFromYAML(nil).TextTailGuard || ConfigFromYAML([]byte("text_tail_guard: false")).TextTailGuard {
		t.Fatal("configuration switch broken")
	}
	strict := map[string]any{"config_name": "deepseek-v4.1-flash", "response_format": map[string]any{"type": "json_object"}}
	if addSOLOTextTailGuard(strict, DefaultConfig(), "id") != "" {
		t.Fatal("must not violate constrained JSON output")
	}
}
