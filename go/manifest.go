package pluginsdk

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Manifest mirrors manifest.toml in a plugin directory. Additional sections
// (config schema, UI declaration) will be added as the host side grows; the
// [plugin] block is the part plugins need to declare today.
type Manifest struct {
	Plugin PluginInfo `toml:"plugin"`
}

// PluginInfo describes one plugin.
type PluginInfo struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
	// Kind is the extension point the plugin implements, e.g. "notifier".
	// Deprecated in favor of Services; kept for backward compatibility.
	Kind string `toml:"kind"`
	// Services lists the extension points (capabilities) the plugin
	// implements, e.g. ["notifier"]. One plugin can declare several.
	Services []string `toml:"services"`
	// ABI is the SDK ABI version (ABIVersion).
	ABI string `toml:"abi"`
	// Config declares the plugin's configuration schema. The host renders
	// it as a form in the admin UI and stores the values; the plugin fetches
	// them at runtime via Config. Keys map field names to field specs.
	Config map[string]ConfigField `toml:"config"`
	// History lists the released versions (changelog), newest first. The
	// admin UI shows it in the version-history dialog. Declared as
	// [[plugin.history]] tables in manifest.toml.
	History []HistoryEntry `toml:"history"`
}

// HistoryEntry describes one released plugin version.
type HistoryEntry struct {
	Version     string `toml:"version" json:"version"`
	Date        string `toml:"date,omitempty" json:"date,omitempty"`
	Description string `toml:"description" json:"description"`
}

// ServiceList returns the declared services; when Services is empty it
// falls back to the legacy single Kind value.
func (p PluginInfo) ServiceList() []string {
	if len(p.Services) > 0 {
		return append([]string(nil), p.Services...)
	}
	if p.Kind != "" {
		return []string{p.Kind}
	}
	return nil
}

// ImplementsService reports whether the plugin declares the given
// extension point.
func (p PluginInfo) ImplementsService(name string) bool {
	for _, s := range p.ServiceList() {
		if s == name {
			return true
		}
	}
	return false
}

// ConfigField describes one configuration entry. Type is one of "string",
// "number" or "boolean". Default is the initial value used when the admin
// has not configured the field yet.
type ConfigField struct {
	// Key is the field name (from the manifest map key); not stored in
	// TOML, filled in by the loader.
	Key     string `toml:"-"`
	Type    string `toml:"type"`
	Label   string `toml:"label"`
	Default any    `toml:"default"`
}

// LoadManifest reads and parses manifest.toml at the given path.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	for key, field := range m.Plugin.Config {
		field.Key = key
		m.Plugin.Config[key] = field
	}
	return &m, nil
}

// Validate performs basic sanity checks on the manifest. A plugin may
// declare no services at all: it then only implements the lifecycle
// (Init/hooks) and host facilities (config/log/data), e.g. plain demo
// plugins.
func (m *Manifest) Validate() error {
	if m.Plugin.Name == "" {
		return fmt.Errorf("manifest: plugin.name is required")
	}
	if m.Plugin.Version == "" {
		return fmt.Errorf("manifest: plugin.version is required")
	}
	if m.Plugin.ABI == "" {
		return fmt.Errorf("manifest: plugin.abi is required")
	}
	if m.Plugin.ABI != ABIVersion {
		return fmt.Errorf("manifest: incompatible plugin.abi %q (SDK speaks %s)", m.Plugin.ABI, ABIVersion)
	}
	return nil
}
