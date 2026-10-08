package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// The measured SOLO DeepSeek path sometimes withholds its last few text
// tokens, yet reports done/stop. A suffix puts disposable tokens at that tail.
// This is a transport workaround, not a reconstruction of missing text. The
// random per-request marker is required before a text answer can be called
// complete, and there is no second, billable request.
func addSOLOTextTailGuard(body map[string]any, cfg Config, completionID string) string {
	if !cfg.TextTailGuard || !strings.EqualFold(readStringField(body, "config_name"), "deepseek-v4.1-flash") {
		return ""
	}
	// A constrained output grammar cannot emit an out-of-band text trailer.
	// Do not violate the caller's JSON/schema contract.
	if format, ok := asMap(body["response_format"]); ok && readStringField(format, "type") != "text" {
		return ""
	}
	id := sha256.Sum256([]byte(completionID))
	marker := fmt.Sprintf("[CPA_END_%x]", id[:4])
	trailer := marker + strings.Repeat(" padding", 16)
	instruction := "Mandatory transport framing for every final text answer, including exact-string, JSON and code answers: first write the complete requested answer; then write a newline followed by this exact trailer: " + trailer + ". You MUST output all 16 padding words and MUST NOT stop before the trailer. The gateway removes the trailer, so the user's requested answer remains exact. Never put framing inside tool arguments. On a native tool-call turn, emit only the native tool call without any trailer."
	messages, _ := asSlice(body["messages"])
	framing := map[string]any{"role": "system", "content": []any{map[string]any{"type": "text", "text": instruction}}}
	index := 0
	for index < len(messages) {
		message, ok := asMap(messages[index])
		if !ok || readStringField(message, "role") != "system" {
			break
		}
		index++
	}
	framed := make([]any, 0, len(messages)+1)
	framed = append(framed, messages[:index]...)
	framed = append(framed, framing)
	body["messages"] = append(framed, messages[index:]...)
	return marker
}

// filterSOLOTextTail runs before either OpenAI response conversion. The ABI
// already buffers both streaming and non-streaming responses, so the marker
// can cross any number of SOLO output fragments without leaking to clients.
// Native tool data, reasoning and the actual billed usage are preserved.
func filterSOLOTextTail(body []byte, marker string) ([]byte, *SOLOStreamError) {
	scanner := &SOLOScanner{}
	events := scanner.Feed(body)
	if tail, ok := scanner.Flush(); ok {
		events = append(events, tail)
	}
	if len(events) == 0 {
		return body, nil // existing empty_upstream handling owns this case
	}
	var text strings.Builder
	tools := &soloToolAccumulator{}
	sawDone, malformed := false, false
	for _, event := range events {
		switch event.Event {
		case soloEventOutput:
			text.WriteString(event.Response)
			malformed = malformed || event.RawData == nil
			for _, call := range event.ToolCalls {
				tools.add(call)
			}
		case soloEventDone:
			sawDone = true
		case soloEventError:
			return body, nil // existing vendor error mapping owns this case
		}
	}
	if !sawDone && len(tools.calls) > 0 {
		return nil, &SOLOStreamError{Message: "工具调用流缺少 done 结束事件，未转发可能不完整的调用；不自动重放"}
	}
	content := text.String()
	keep := len(content)
	verified := marker == ""
	if marker != "" {
		// Require a trailer line rather than an incidental inline mention.
		if index := strings.Index(content, "\n"+marker); index >= 0 {
			keep = len(strings.TrimRight(content[:index], "\r\n"))
			verified = true
		}
		if !verified {
			// A cut inside the private marker is still incomplete, but the
			// internal framing must not become visible answer text.
			for size := len(marker) - 1; size >= len("[CPA_END_")+3; size-- {
				if strings.HasSuffix(content, marker[:size]) {
					index := len(content) - size
					if index > 0 && content[index-1] == '\n' {
						keep = len(strings.TrimRight(content[:index], "\r\n"))
					}
					break
				}
			}
		}
	}
	incomplete := !sawDone || malformed || (!verified && len(tools.calls) == 0)
	if marker == "" && !incomplete {
		return body, nil
	}
	var result bytes.Buffer
	remaining := keep
	for _, event := range events {
		raw := event.RawData
		if raw == nil {
			raw = map[string]any{}
		}
		if event.Event == soloEventOutput && event.Response != "" {
			count := len(event.Response)
			if count > remaining {
				count = remaining
			}
			raw["response"] = event.Response[:count]
			remaining -= count
		}
		if event.Event == soloEventDone && incomplete {
			if reason := readStringField(raw, "finish_reason"); reason == "" || reason == "stop" || reason == "tool_calls" {
				raw["finish_reason"] = "length"
			}
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, &SOLOStreamError{Message: "无法编码 SOLO 响应事件"}
		}
		fmt.Fprintf(&result, "event: %s\ndata: %s\n\n", event.Event, encoded)
	}
	if !sawDone {
		result.WriteString("event: done\ndata: {\"finish_reason\":\"length\"}\n\n")
	}
	return result.Bytes(), nil
}
