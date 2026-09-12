package pluginsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"
)

// ErrHostUnavailable is returned by host helpers when the host connection
// has not been established yet (Init has not run or failed).
var ErrHostUnavailable = errors.New("pluginsdk: host connection not established")

// ErrNoConfig is returned by CurrentConfig when the host has not delivered
// any configuration (no config stored yet and Init received none).
var ErrNoConfig = errors.New("pluginsdk: no configuration available")

// ErrNoData is returned by Data when the key does not exist.
var ErrNoData = errors.New("pluginsdk: no data for key")

// Status values accepted by ReportStatus.
const (
	StatusOK            = "ok"
	StatusError         = "error"
	StatusConfigInvalid = "config_invalid"
)

var (
	hostMu     sync.RWMutex
	hostConn   *grpc.ClientConn
	hostClient gen.HostServiceClient

	// cfgMu guards the config state below: cfgRaw (the cached plugin
	// configuration, filled at Init from the config_json pushed by the host
	// and updated on ConfigChanged), cfgDelivered (whether the first
	// delivery happened) and cfgReceivers (typed OnConfig receivers).
	cfgMu        sync.RWMutex
	cfgRaw       string
	cfgDelivered bool
	cfgReceivers []configReceiver
)

// configReceiver is a registered OnConfig callback: it decodes the cached
// configuration into the callback's own type and invokes it.
type configReceiver struct {
	invoke func(ctx context.Context) error
}

// installHostClient stores the connection to the host's HostService. Called
// once from the lifecycle Init RPC. Safe to call repeatedly; previous
// connections are closed.
func installHostClient(conn *grpc.ClientConn) {
	hostMu.Lock()
	defer hostMu.Unlock()
	if hostConn != nil {
		hostConn.Close()
	}
	hostConn = conn
	hostClient = gen.NewHostServiceClient(conn)
}

func currentHostClient() (gen.HostServiceClient, error) {
	hostMu.RLock()
	defer hostMu.RUnlock()
	if hostClient == nil {
		return nil, ErrHostUnavailable
	}
	return hostClient, nil
}

// setConfigCache stores the raw configuration JSON ("" clears it).
func setConfigCache(raw string) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	cfgRaw = raw
}

// markConfigDelivered switches the SDK to delivery mode: OnConfig
// registrations from then on are invoked immediately with the cached
// configuration instead of being queued for the first delivery.
func markConfigDelivered() {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	cfgDelivered = true
}

// OnConfig registers a typed configuration receiver — the Go equivalent of
// in-process systems handing the plugin a parsed config dict (e.g.
// MoviePilot's init_plugin(config)). The SDK decodes the delivered config
// JSON into T and calls fn with it:
//
//   - when registered before the host delivers the first configuration
//     (e.g. from func main), fn is called during lifecycle Init, before the
//     plugin's own Init
//   - when registered from inside Init, fn is called immediately with the
//     current configuration
//   - on every later configuration change (admin saved new values), fn is
//     called again with the fresh values
//
// fn runs with panic recovery; its error marks the plugin errored. Use it
// instead of pulling CurrentConfig manually.
func OnConfig[T any](fn func(ctx context.Context, cfg T) error) error {
	if fn == nil {
		return errors.New("pluginsdk: OnConfig function is nil")
	}
	receiver := configReceiver{
		invoke: func(ctx context.Context) error {
			var cfg T
			cfgMu.RLock()
			raw := cfgRaw
			cfgMu.RUnlock()
			if raw != "" {
				if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
					return fmt.Errorf("pluginsdk: decode config for receiver: %w", err)
				}
			}
			return fn(ctx, cfg)
		},
	}

	cfgMu.Lock()
	delivered := cfgDelivered
	cfgMu.Unlock()
	if !delivered {
		cfgMu.Lock()
		cfgReceivers = append(cfgReceivers, receiver)
		cfgMu.Unlock()
		return nil
	}
	// Registered after the first delivery: hand the current config over now
	// (bounded so a stuck receiver cannot hang the plugin) and only then
	// register it — appending first would let a concurrent
	// runConfigReceivers snapshot invoke the same callback in parallel with
	// this immediate call.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := invokeConfigReceiver(ctx, receiver); err != nil {
		return err
	}
	cfgMu.Lock()
	cfgReceivers = append(cfgReceivers, receiver)
	cfgMu.Unlock()
	return nil
}

// runConfigReceivers hands the current cached configuration to every
// registered receiver (lifecycle Init and ConfigChanged).
func runConfigReceivers(ctx context.Context) error {
	cfgMu.RLock()
	receivers := append([]configReceiver(nil), cfgReceivers...)
	cfgMu.RUnlock()
	for _, r := range receivers {
		if err := invokeConfigReceiver(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

func invokeConfigReceiver(ctx context.Context, receiver configReceiver) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = reportPanic(r)
		}
	}()
	return receiver.invoke(ctx)
}

// CurrentConfig returns the cached plugin configuration, delivered by the
// host at Init and updated by RefreshConfig. It never performs an RPC, so
// per-request calls are cheap. Returns ErrNoConfig when the host has not
// configured the plugin yet.
func CurrentConfig[T any]() (T, error) {
	var out T
	cfgMu.RLock()
	raw := cfgRaw
	cfgMu.RUnlock()
	if raw == "" {
		return out, ErrNoConfig
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, fmt.Errorf("pluginsdk: invalid cached config: %w", err)
	}
	return out, nil
}

// RefreshConfig fetches the plugin's current configuration from the host,
// updates the cache (visible to later CurrentConfig calls) and returns it.
// Plugins call it when they want to pick up configuration changes without a
// restart.
func RefreshConfig[T any](ctx context.Context) (T, error) {
	var out T
	client, err := currentHostClient()
	if err != nil {
		return out, err
	}
	resp, err := client.GetConfig(ctx, &gen.GetConfigRequest{PluginId: PluginID()})
	if err != nil {
		return out, err
	}
	if resp.ConfigJson == "" {
		setConfigCache("")
		return out, ErrNoConfig
	}
	// Validate before caching: a failed refresh must not poison the cache
	// that CurrentConfig and OnConfig receivers keep reading.
	if err := json.Unmarshal([]byte(resp.ConfigJson), &out); err != nil {
		return out, fmt.Errorf("pluginsdk: invalid config from host: %w", err)
	}
	setConfigCache(resp.ConfigJson)
	return out, nil
}

// Data returns the plugin-private value stored under key, unmarshaled as
// JSON into T. The host keeps the value in its database under the plugin's
// own namespace, so it survives plugin restarts. Returns ErrNoData when the
// key does not exist.
func Data[T any](ctx context.Context, key string) (T, error) {
	var out T
	client, err := currentHostClient()
	if err != nil {
		return out, err
	}
	resp, err := client.GetData(ctx, &gen.GetDataRequest{PluginId: PluginID(), Key: key})
	if err != nil {
		return out, err
	}
	if resp.ValueJson == "" {
		return out, ErrNoData
	}
	if err := json.Unmarshal([]byte(resp.ValueJson), &out); err != nil {
		return out, fmt.Errorf("pluginsdk: invalid data for key %q: %w", key, err)
	}
	return out, nil
}

// SetData stores value (JSON-marshaled) under key in the plugin's private
// namespace on the host.
func SetData(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("pluginsdk: marshal data for key %q: %w", key, err)
	}
	client, err := currentHostClient()
	if err != nil {
		return err
	}
	_, err = client.SetData(ctx, &gen.SetDataRequest{
		PluginId:  PluginID(),
		Key:       key,
		ValueJson: string(raw),
	})
	return err
}

// DeleteData removes the plugin's key on the host. Deleting a missing key
// is not an error.
func DeleteData(ctx context.Context, key string) error {
	client, err := currentHostClient()
	if err != nil {
		return err
	}
	_, err = client.DeleteData(ctx, &gen.DeleteDataRequest{PluginId: PluginID(), Key: key})
	return err
}

// Log sends a structured log line to the host logger. Prefer the typed
// helpers LogDebug/LogInfo/LogWarn/LogError. Failures fall back to the
// standard logger (stderr).
func Log(level, message string) {
	client, err := currentHostClient()
	if err == nil {
		// Best-effort RPC: bound it so a half-dead connection or a slow host
		// cannot block the caller (and the panic-recovery path that uses it).
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = client.Log(ctx, &gen.LogRequest{
			PluginId: PluginID(),
			Level:    level,
			Message:  message,
		})
		cancel()
		if err == nil {
			return
		}
	}
	log.Printf("[%s] %s", level, message)
}

// formatLogArgs mirrors the host project's logger behavior: sprintf-style
// formatting when args are present, the message verbatim otherwise.
func formatLogArgs(msg string, args ...any) string {
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}

// LogDebug logs a debug-level line (visible when the host log level is
// debug). Signature matches the host project's logger.Debug.
func LogDebug(msg string, args ...any) { Log("debug", formatLogArgs(msg, args...)) }

// LogInfo logs an info-level line.
func LogInfo(msg string, args ...any) { Log("info", formatLogArgs(msg, args...)) }

// LogWarn logs a warn-level line.
func LogWarn(msg string, args ...any) { Log("warn", formatLogArgs(msg, args...)) }

// LogError logs an error-level line.
func LogError(msg string, args ...any) { Log("error", formatLogArgs(msg, args...)) }

// ReportStatus reports the plugin's runtime status to the host, which
// surfaces it in the admin UI. status should be one of StatusOK,
// StatusError or StatusConfigInvalid. Failures fall back to the standard
// logger (stderr).
func ReportStatus(status, detail string) {
	client, err := currentHostClient()
	if err == nil {
		// Best-effort RPC with the same bound as Log: a stuck status report
		// must not hang the caller or the panic-recovery path.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = client.ReportStatus(ctx, &gen.ReportStatusRequest{
			PluginId: PluginID(),
			Status:   status,
			Error:    detail,
		})
		cancel()
		if err == nil {
			return
		}
	}
	log.Printf("[status:%s] %s", status, detail)
}

func checkABI(version string) error {
	if version == "" || version == ABIVersion {
		return nil
	}
	return fmt.Errorf("pluginsdk: incompatible ABI version %q (plugin speaks %s)", version, ABIVersion)
}
