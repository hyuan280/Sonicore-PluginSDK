// Package pluginsdk is the Sonicore plugin SDK. Plugin authors implement
// the minimal Plugin interface (Init) — plus Notifier when the plugin
// provides a notification channel — and call Run from func main. Inside
// Init the plugin declares what it provides (ProvideNotifier, schedules
// via RegisterSchedule); the SDK takes care of the go-plugin handshake,
// the bidirectional gRPC wiring and the host-service helpers
// (configuration, logging, status reporting, plugin-private data).
//
//	func main() {
//		pluginsdk.Run("my-plugin", &MyPlugin{})
//	}
//
//	func (p *MyPlugin) Init(ctx context.Context) error {
//		pluginsdk.ProvideNotifier(p) // MyPlugin implements Notifier
//		return nil
//	}
//
// stdout/stderr contract: the host reads ONLY the go-plugin handshake line
// from stdout — anything else the plugin prints is discarded. Plugins must
// therefore report their own diagnostics: Log/LogError for messages,
// ReportStatus for status changes, and rely on Run/Main's panic recovery
// (panics are logged and reported to the host, which marks the plugin
// errored). Direct writes to stderr will not reach the host's logs.
//
// The generated ABI stubs live in the gen subpackage and are shared with the
// host, so both sides always speak the same protocol.
package pluginsdk

import (
	"log"
	"os"
	"runtime/debug"
)

// ABI version. Both sides check this during Init and refuse to talk when
// they disagree.
const ABIVersion = "sonicore.plugin.v1"

// Environment variables injected by the host when it spawns a plugin.
const (
	// PluginIDEnv holds the host-assigned unique plugin instance id, used to
	// address this plugin's configuration and status on the host.
	PluginIDEnv = "SONICORE_PLUGIN_ID"
	// PluginDirEnv holds the plugin's own directory (private writable data).
	PluginDirEnv = "SONICORE_PLUGIN_DIR"
)

// PluginID returns the host-assigned plugin instance id (empty when the
// process was not started as a plugin).
func PluginID() string { return os.Getenv(PluginIDEnv) }

// PluginDir returns the plugin's private directory injected by the host.
func PluginDir() string { return os.Getenv(PluginDirEnv) }

// Main wraps the plugin entry point. Use it from func main to catch panics
// during startup (ProvideNotifier, handshake, Init) that happen before the
// host connection is fully established: they cannot be reported through
// HostService yet, so Main logs them to stderr and exits non-zero, letting
// the host mark the plugin errored. Per the package stdout/stderr contract
// the host drops stderr output, so the panic detail is only visible
// locally.
//
// Example:
//
//	func main() {
//		pluginsdk.Main(func() {
//			pluginsdk.ProvideNotifier(&MyNotifier{})
//		})
//	}
func Main(run func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[fatal] panic in plugin main: %v\n%s", r, debug.Stack())
			os.Exit(1)
		}
	}()
	run()
}
