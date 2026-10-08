package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/collegeming/cpa-jethub-plugins/internal/abiboot"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestCatalogConfiguredChannels(t *testing.T) {
	body := batchJSON(
		"solo_agent", []string{plainEntryJSON("shared"), effortEntryJSON("current", []string{"light", "high"})},
		"git_ai", []string{effortEntryJSON("shared", []string{"extra_high"}), plainEntryJSON("legacy")},
	)
	models := ParseBatchModelListForChannels(body, 1000, []string{"solo_agent"})
	if len(models) != 2 || models[0].ID != "shared" || models[1].ID != "current" {
		t.Fatalf("non-SOLO models leaked: %#v", models)
	}
	if models[0].Channel != "solo_agent" || models[0].Reasoning != nil {
		t.Fatalf("unconfigured channel overwrote SOLO metadata: %#v", models[0])
	}
	if len(ParseBatchModelListForChannels(body, 1000, []string{"git_ai"})) != 2 {
		t.Fatal("an explicitly configured alternate channel must still work")
	}
	for _, raw := range [][]byte{nil, []byte("bad"), []byte(`{}`), body} {
		if len(ParseBatchModelListForChannels(raw, 1000, nil)) != 0 {
			t.Fatal("missing configured channels must not expose the entire IDE catalog")
		}
	}
}

func TestCatalogStatusMetadataOnly(t *testing.T) {
	previous := settings()
	setSettings(DefaultConfig())
	t.Cleanup(func() { setSettings(previous) })
	credential, err := ParseCredential(credentialJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	invalidateCatalog(settings(), credential)
	var requests []string
	accountHost(t, &requests)
	response := statusJSON(abiboot.NewHost(nil), pluginapi.ManagementRequest{Query: url.Values{"models_only": {"1"}}})
	var document struct {
		Models []pluginapi.ModelInfo `json:"models"`
		Source string                `json:"model_source"`
		Count  int                   `json:"model_count"`
	}
	if json.Unmarshal(response.Body, &document) != nil || document.Source != "server" || document.Count != 2 || len(document.Models) != 2 {
		t.Fatalf("missing live descriptors: %s", response.Body)
	}
	model := document.Models[0]
	if model.ID != "glm-5.1" || model.ContextLength != 1000000 || model.OutputTokenLimit != 384000 || model.Thinking == nil || len(model.Thinking.Levels) != 3 {
		t.Fatalf("capabilities lost: %#v", model)
	}
	if len(requests) != 1 || !strings.Contains(requests[0], BatchModelsPath) {
		t.Fatalf("model-only status must not read credits or checkin: %#v", requests)
	}
	for _, secret := range []string{`"tok"`, `"refresh"`, `"access_token"`, `"refresh_token"`} {
		if strings.Contains(string(response.Body), secret) {
			t.Fatal("model metadata exposed credential fields")
		}
	}
}

func TestCatalogStatusLabelsFallback(t *testing.T) {
	previous := settings()
	cfg := DefaultConfig()
	cfg.DiscoverModels = false
	setSettings(cfg)
	t.Cleanup(func() { setSettings(previous) })
	var requests []string
	accountHost(t, &requests)
	response := statusJSON(abiboot.NewHost(nil), pluginapi.ManagementRequest{Query: url.Values{"models_only": {"1"}}})
	var document map[string]any
	if json.Unmarshal(response.Body, &document) != nil || document["model_source"] != "fallback" {
		t.Fatalf("fallback advertised as live: %s", response.Body)
	}
	if len(requests) != 0 {
		t.Fatalf("disabled discovery called upstream: %#v", requests)
	}
}
