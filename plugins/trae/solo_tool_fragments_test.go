package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type fragmentTestCall struct {
	ID        string
	Name      string
	Arguments string
}

func fragmentTestEntry(index any, id, name, arguments string) map[string]any {
	call := map[string]any{"type": "function", "function_call": map[string]any{
		"name": name, "arguments": arguments, "partial_arguments": nil,
	}}
	if index != nil {
		call["index"] = index
	}
	if id != "" {
		call["id"] = id
	}
	return call
}

func fragmentTestBody(t *testing.T, events [][]map[string]any) []byte {
	t.Helper()
	var body bytes.Buffer
	for _, calls := range events {
		encoded, err := json.Marshal(map[string]any{"tool_calls": calls})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, "event: output\ndata: %s\n\n", encoded)
	}
	return body.Bytes()
}

func TestSOLOToolCallFragments(t *testing.T) {
	cases := []struct {
		name   string
		events [][]map[string]any
		want   map[int]fragmentTestCall
	}{
		{
			name: "live_shape_empty_names_on_argument_continuations",
			events: [][]map[string]any{
				{fragmentTestEntry(0, "call_add", "add", "")},
				{fragmentTestEntry(0, "", "", `{"a": 19, "b`)},
				{fragmentTestEntry(0, "", "", `": 23}`)},
			},
			want: map[int]fragmentTestCall{0: {"call_add", "add", `{"a": 19, "b": 23}`}},
		},
		{
			name: "interleaved_sparse_indices_do_not_mix_arguments",
			events: [][]map[string]any{
				{fragmentTestEntry(5, "call_b", "multiply", `{"x":`), fragmentTestEntry(2, "call_a", "add", `{"a":`)},
				{fragmentTestEntry(2, "", "", `19,"b":23}`)},
				{fragmentTestEntry(5, "", "", `2,"y":3}`)},
			},
			want: map[int]fragmentTestCall{2: {"call_a", "add", `{"a":19,"b":23}`}, 5: {"call_b", "multiply", `{"x":2,"y":3}`}},
		},
		{
			name: "id_only_continuation_preserves_original_index",
			events: [][]map[string]any{
				{fragmentTestEntry(3, "call_a", "add", `{"a":`)},
				{fragmentTestEntry(nil, "call_a", "", `19,"b":23}`)},
			},
			want: map[int]fragmentTestCall{3: {"call_a", "add", `{"a":19,"b":23}`}},
		},
		{
			name: "missing_id_has_stable_fallback_even_if_id_arrives_later",
			events: [][]map[string]any{
				{fragmentTestEntry(0, "", "add", `{"a":`)},
				{fragmentTestEntry(0, "real_id_later", "", `19,"b":23}`)},
			},
			want: map[int]fragmentTestCall{0: {"call_0", "add", `{"a":19,"b":23}`}},
		},
		{
			name: "repeated_metadata_is_not_concatenated_twice",
			events: [][]map[string]any{
				{fragmentTestEntry(0, "call_a", "add", `{"a":`)},
				{fragmentTestEntry(0, "call_a", "add", `19,"b":23}`)},
			},
			want: map[int]fragmentTestCall{0: {"call_a", "add", `{"a":19,"b":23}`}},
		},
		{
			name: "unknown_unnamed_call_is_not_a_continuation",
			events: [][]map[string]any{
				{fragmentTestEntry(0, "call_a", "add", `{"a":19,"b":23}`)},
				{fragmentTestEntry(7, "orphan", "", `{"bad":true}`)},
			},
			want: map[int]fragmentTestCall{0: {"call_a", "add", `{"a":19,"b":23}`}},
		},
		{
			name: "conflicting_id_cannot_poison_existing_index",
			events: [][]map[string]any{
				{fragmentTestEntry(0, "call_a", "add", `{"a":19,"b":23}`)},
				{fragmentTestEntry(0, "different_id", "", `{"bad":true}`)},
			},
			want: map[int]fragmentTestCall{0: {"call_a", "add", `{"a":19,"b":23}`}},
		},
		{
			name: "independent_calls_without_indices_stay_separate",
			events: [][]map[string]any{
				{fragmentTestEntry(nil, "call_a", "add", `{"a":19,"b":23}`)},
				{fragmentTestEntry(nil, "call_b", "multiply", `{"x":2,"y":3}`)},
			},
			want: map[int]fragmentTestCall{0: {"call_a", "add", `{"a":19,"b":23}`}, 1: {"call_b", "multiply", `{"x":2,"y":3}`}},
		},
		{
			name: "id_cannot_move_to_another_index",
			events: [][]map[string]any{
				{fragmentTestEntry(3, "call_a", "add", `{"a":19,"b":23}`)},
				{fragmentTestEntry(7, "call_a", "", `{"bad":true}`)},
			},
			want: map[int]fragmentTestCall{3: {"call_a", "add", `{"a":19,"b":23}`}},
		},
		{
			name: "conflicting_name_cannot_poison_existing_call",
			events: [][]map[string]any{
				{fragmentTestEntry(0, "call_a", "add", `{"a":19,"b":23}`)},
				{fragmentTestEntry(0, "call_a", "multiply", `{"bad":true}`)},
			},
			want: map[int]fragmentTestCall{0: {"call_a", "add", `{"a":19,"b":23}`}},
		},
		{
			name: "no_argument_tool_keeps_metadata_only_once",
			events: [][]map[string]any{
				{fragmentTestEntry(0, "call_a", "add", `{}`)},
				{fragmentTestEntry(0, "call_a", "add", "")},
			},
			want: map[int]fragmentTestCall{0: {"call_a", "add", `{}`}},
		},
		{
			name:   "orphan_only_never_becomes_an_executable_call",
			events: [][]map[string]any{{fragmentTestEntry(0, "orphan", "", `{"bad":true}`)}},
			want:   map[int]fragmentTestCall{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fragmentTestBody(t, tc.events)
			completion, completionErr, saw := AggregateSOLOToCompletion(body, "deepseek-v4.1-flash", "test", 0)
			if completionErr != nil || !saw {
				t.Fatalf("nonstream: error=%v saw=%v", completionErr, saw)
			}
			choices, _ := asSlice(completion["choices"])
			choice := mustMap(t, choices[0])
			message := mustMap(t, choice["message"])
			calls, _ := asSlice(message["tool_calls"])
			got := map[int]fragmentTestCall{}
			for _, value := range calls {
				call := mustMap(t, value)
				index, _ := readNumberField(call, "index")
				name, arguments, id := toolCallFields(call)
				if _, duplicate := got[int(index)]; duplicate {
					t.Fatalf("duplicate aggregate index: %v", index)
				}
				got[int(index)] = fragmentTestCall{id, name, arguments}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("nonstream calls=%#v want=%#v", got, tc.want)
			}
			frames, streamErr, streamSaw := TranslateSOLOStream(body, "deepseek-v4.1-flash", "test", 0)
			if streamErr != nil || !streamSaw {
				t.Fatalf("stream: error=%v saw=%v", streamErr, streamSaw)
			}
			got = map[int]fragmentTestCall{}
			for _, frame := range frames {
				var chunk openAIChunk
				if err := json.Unmarshal(frame, &chunk); err != nil {
					t.Fatal(err)
				}
				for _, choice := range chunk.Choices {
					calls, _ := asSlice(choice.Delta["tool_calls"])
					for _, value := range calls {
						call := mustMap(t, value)
						index, _ := readNumberField(call, "index")
						name, arguments, id := toolCallFields(call)
						current := got[int(index)]
						current.ID += id
						current.Name += name
						current.Arguments += arguments
						got[int(index)] = current
					}
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("stream calls=%#v want=%#v", got, tc.want)
			}
			wantFinish := "tool_calls"
			if len(tc.want) == 0 {
				wantFinish = "length"
			}
			if choice["finish_reason"] != wantFinish || translateFinishReason(t, frames) != wantFinish {
				t.Errorf("finish nonstream=%v stream=%v want=%v", choice["finish_reason"], translateFinishReason(t, frames), wantFinish)
			}
		})
	}
}

func TestSOLOToolCallFinishReasons(t *testing.T) {
	for _, explicit := range []string{"stop", "length", "tool_calls", "content_filter"} {
		t.Run(explicit, func(t *testing.T) {
			body := fragmentTestBody(t, [][]map[string]any{{fragmentTestEntry(0, "call_a", "add", `{"a":19,"b":23}`)}})
			body = append(body, []byte(fmt.Sprintf("event: done\ndata: {\"finish_reason\":%q}\n\n", explicit))...)
			want := explicit
			if want == "stop" {
				want = "tool_calls"
			}
			completion, err, _ := AggregateSOLOToCompletion(body, "model", "test", 0)
			if err != nil {
				t.Fatal(err)
			}
			choices, _ := asSlice(completion["choices"])
			if got := mustMap(t, choices[0])["finish_reason"]; got != want {
				t.Errorf("nonstream finish=%v want=%s", got, want)
			}
			frames, err, _ := TranslateSOLOStream(body, "model", "test", 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := translateFinishReason(t, frames); got != want {
				t.Errorf("stream finish=%s want=%s", got, want)
			}
		})
	}
}
