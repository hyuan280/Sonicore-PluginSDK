package pluginsdk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.toml")
	content := `
[plugin]
name = "telegram"
version = "1.0.0"
description = "Telegram notifications"
author = "someone"
kind = "notifier"
abi = "sonicore.plugin.v1"

[plugin.config]
bot_token = { type = "string", label = "Bot Token" }
chat_id = { type = "string", label = "Chat ID", default = "-1" }
verbose = { type = "boolean", default = true }
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if m.Plugin.Name != "telegram" || m.Plugin.Kind != "notifier" {
		t.Fatalf("unexpected manifest: %+v", m.Plugin)
	}
	if len(m.Plugin.Config) != 3 {
		t.Fatalf("expected 3 config fields, got %d", len(m.Plugin.Config))
	}
	bot := m.Plugin.Config["bot_token"]
	if bot.Key != "bot_token" || bot.Type != "string" || bot.Label != "Bot Token" {
		t.Fatalf("unexpected config field: %+v", bot)
	}
	if got := m.Plugin.Config["chat_id"].Default; got != "-1" {
		t.Fatalf("unexpected default: %v", got)
	}
	if got := m.Plugin.Config["verbose"].Default; got != true {
		t.Fatalf("unexpected boolean default: %v", got)
	}
}

func TestManifestValidateMissingFields(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Manifest)
	}{
		{"name", func(m *Manifest) { m.Plugin.Name = "" }},
		{"version", func(m *Manifest) { m.Plugin.Version = "" }},
		{"abi", func(m *Manifest) { m.Plugin.ABI = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{Plugin: PluginInfo{
				Name: "x", Version: "1", Kind: "notifier", ABI: ABIVersion,
			}}
			tc.edit(m)
			if err := m.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestManifestValidateWithoutServices(t *testing.T) {
	// A plain plugin without any declared service is valid: it only
	// implements the lifecycle.
	m := &Manifest{Plugin: PluginInfo{Name: "demo", Version: "1", ABI: ABIVersion}}
	if err := m.Validate(); err != nil {
		t.Fatalf("service-less plugin should validate: %v", err)
	}
	if len(m.Plugin.ServiceList()) != 0 {
		t.Fatalf("expected empty service list, got %v", m.Plugin.ServiceList())
	}
}

func TestManifestServices(t *testing.T) {
	// Legacy single kind falls back into the service list.
	legacy := PluginInfo{Kind: "notifier"}
	if !legacy.ImplementsService("notifier") || legacy.ImplementsService("metadata") {
		t.Fatalf("legacy kind fallback broken: %v", legacy.ServiceList())
	}

	// Services array works and supports multiple capabilities.
	multi := PluginInfo{Services: []string{"notifier", "metadata"}}
	if !multi.ImplementsService("notifier") || !multi.ImplementsService("metadata") {
		t.Fatalf("services array broken: %v", multi.ServiceList())
	}

	// Services wins over Kind when both are set.
	both := PluginInfo{Kind: "metadata", Services: []string{"notifier"}}
	if !both.ImplementsService("notifier") || both.ImplementsService("metadata") {
		t.Fatalf("services should take precedence: %v", both.ServiceList())
	}
}

func TestCheckABI(t *testing.T) {
	if err := checkABI(""); err != nil {
		t.Fatalf("empty ABI should be accepted: %v", err)
	}
	if err := checkABI(ABIVersion); err != nil {
		t.Fatalf("matching ABI should be accepted: %v", err)
	}
	if err := checkABI("other.v2"); err == nil {
		t.Fatal("mismatched ABI should be rejected")
	}
}

func TestManifestValidateRejectsWrongABI(t *testing.T) {
	m := &Manifest{Plugin: PluginInfo{Name: "x", Version: "1", ABI: "sonicore.plugin.v2"}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected validation error for mismatched abi")
	}
}

func TestServiceListReturnsCopy(t *testing.T) {
	info := PluginInfo{Services: []string{"notifier", "metadata"}}
	list := info.ServiceList()
	list[0] = "mutated"
	if info.Services[0] != "notifier" {
		t.Fatalf("ServiceList shares storage with the manifest: %v", info.Services)
	}
}
