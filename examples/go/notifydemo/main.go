// Package main is the reference "notifydemo" notification plugin: it
// implements the full plugin contract — Plugin.Init (declares the notifier
// via ProvideNotifier, receives configuration via OnConfig),
// Notifier.Send and the optional ShutdownAware hook — and only logs. The
// plugin keeps its own configuration copy, handed over by the SDK whenever
// the host delivers it (init + changes).
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/hyuan280/Sonicore-PluginSDK/go"
	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"
)

// notifyDemoConfig is declared in manifest.toml ([plugin.config]); the host
// renders it as a form in the admin UI, pushes the values with Init and
// notifies changes via ConfigChanged. The SDK decodes them into this type
// and hands them to the OnConfig receiver.
type notifyDemoConfig struct {
	Greeting string `json:"greeting"`
	Level    string `json:"level"`
	ToStdout bool   `json:"to_stdout"`
}

type notifyDemo struct {
	// The plugin's own configuration copy, updated by setConfig.
	mu  sync.RWMutex
	cfg notifyDemoConfig
}

// Init implements pluginsdk.Plugin. The SDK calls it after the host
// connection is ready: the plugin declares what it provides
// (ProvideNotifier) and registers the typed config receiver (OnConfig),
// which the SDK invokes immediately with the current configuration.
func (n *notifyDemo) Init(ctx context.Context) error {
	pluginsdk.ProvideNotifier(n)
	if err := pluginsdk.OnConfig(n.setConfig); err != nil {
		return err
	}
	pluginsdk.LogInfo("notifydemo initialized (plugin=%s)", pluginsdk.PluginID())
	return nil
}

// setConfig is the typed configuration receiver: the SDK calls it at Init
// and on every configuration change with the decoded values.
func (n *notifyDemo) setConfig(ctx context.Context, cfg notifyDemoConfig) error {
	n.mu.Lock()
	n.cfg = cfg
	n.mu.Unlock()
	pluginsdk.LogInfo("config received: greeting=%q level=%q", cfg.Greeting, cfg.Level)
	return nil
}

// Send implements Notifier: it logs the received notification and keeps a
// plugin-private counter in the host database (survives restarts).
func (n *notifyDemo) Send(ctx context.Context, msg *gen.NotificationMessage) error {
	n.mu.RLock()
	cfg := n.cfg
	n.mu.RUnlock()

	count := 0
	if v, err := pluginsdk.Data[int](ctx, "send_count"); err == nil {
		count = v
	} else if err != pluginsdk.ErrNoData {
		pluginsdk.LogWarn("load send_count failed: %v", err)
	}
	count++
	if err := pluginsdk.SetData(ctx, "send_count", count); err != nil {
		pluginsdk.LogWarn("save send_count failed: %v", err)
	}

	greeting := cfg.Greeting
	if greeting == "" {
		greeting = "Hello"
	}
	if cfg.ToStdout {
		fmt.Printf("%s from %s: %q (%s) -> %v\n", greeting, pluginsdk.PluginID(), msg.Subject, msg.Type, msg.To)
	}
	switch cfg.Level {
	case "debug":
		pluginsdk.LogDebug("%s from %s (#%d): %q (%s) -> %v", greeting, pluginsdk.PluginID(), count, msg.Subject, msg.Type, msg.To)
	case "warn":
		pluginsdk.LogWarn("%s from %s (#%d): %q (%s) -> %v", greeting, pluginsdk.PluginID(), count, msg.Subject, msg.Type, msg.To)
	case "error":
		pluginsdk.LogError("%s from %s (#%d): %q (%s) -> %v", greeting, pluginsdk.PluginID(), count, msg.Subject, msg.Type, msg.To)
	default:
		pluginsdk.LogInfo("%s from %s (#%d): %q (%s) -> %v", greeting, pluginsdk.PluginID(), count, msg.Subject, msg.Type, msg.To)
	}
	pluginsdk.ReportStatus(pluginsdk.StatusOK, "")
	return nil
}

// Shutdown implements the optional ShutdownAware hook; it runs during a
// graceful stop before the process exits.
func (n *notifyDemo) Shutdown(ctx context.Context) error {
	pluginsdk.LogInfo("notifydemo shutting down, bye")
	return nil
}

func main() {
	pluginsdk.Run("notifydemo", &notifyDemo{})
}
