package pluginsdk

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"
)

// Notifier is the interface a notification-channel plugin implements. The
// host calls Send once per notification routed to this channel. Returning an
// error marks the delivery as failed; the host decides how to retry.
//
// Panics inside Send are caught by the SDK and reported to the host
// (LogError + ReportStatus), so a faulty plugin does not silently kill the
// process.
type Notifier interface {
	Send(ctx context.Context, msg *gen.NotificationMessage) error
}

// Plugin is the minimal interface every plugin implements: the SDK calls
// Init after the host connection and the configuration cache are ready.
// Inside Init the plugin declares what it provides (e.g. ProvideNotifier)
// and sets up its own resources. Returning an error marks the plugin
// errored on the host.
type Plugin interface {
	Init(ctx context.Context) error
}

// ShutdownAware is an optional interface plugins can implement to run
// cleanup when the host asks the plugin to stop gracefully (disable,
// uninstall or server shutdown). The hook runs before the process exits; a
// timeout bounds it. Implementing it is optional: without it the plugin
// simply exits.
type ShutdownAware interface {
	Shutdown(ctx context.Context) error
}

// ConfigChangeAware is an optional interface plugins can implement to react
// to configuration updates pushed by the host (admin saved new values in
// the UI). The SDK refreshes its config cache before calling the hook, so
// CurrentConfig already returns the new values inside it. Use it to rebuild
// long-lived resources such as sessions or API clients; return an error to
// have it logged and the plugin marked with an error status.
type ConfigChangeAware interface {
	OnConfigChanged(ctx context.Context) error
}

// Run starts the plugin process: it wraps Main (panic recovery during
// startup), serves the shared lifecycle (Init callback, config, data,
// logging, schedules) and blocks until the host shuts the plugin down.
//
// What the plugin provides is declared by the plugin itself inside its
// Init (e.g. ProvideNotifier to serve notification channels); Run performs
// no capability detection on impl.
//
//	func main() {
//		pluginsdk.Run("my-plugin", &MyPlugin{})
//	}
//
//	type MyPlugin struct{}
//
//	func (p *MyPlugin) Init(ctx context.Context) error {
//		pluginsdk.ProvideNotifier(p) // MyPlugin also implements Notifier
//		return nil
//	}
func Run(name string, impl any) {
	Main(func() {
		serve(name, impl)
	})
}

// ProvideNotifier declares that this plugin serves notifications: the
// NotifierService routes Send calls to n. Call it from Init (the recommended
// place — the host starts routing only after Init returns) or from func
// main before Run. Send calls arriving before a handler is provided fail
// with a clear error.
func ProvideNotifier(n Notifier) {
	notifierMu.Lock()
	notifierImpl = n
	notifierMu.Unlock()
}

// serve starts the go-plugin server for one plugin. The shared services
// (lifecycle, tasks, notifier) are always registered; whether the notifier
// service actually answers depends on ProvideNotifier having been called.
func serve(name string, impl any) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins: map[string]plugin.Plugin{
			name: &servedPlugin{impl: impl},
		},
		// gRPC serving for this plugin. A non-nil value enables it.
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

// servedPlugin adapts the plugin implementation to go-plugin's GRPCPlugin.
// The plugin process only ever serves (GRPCServer); its GRPCClient method
// is never invoked by the framework on the plugin side, but the host uses
// the same type to obtain the client stubs, so keep it.
type servedPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	impl any
}

func (p *servedPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	gen.RegisterLifecycleServiceServer(s, newLifecycleServer(p.impl, broker))
	gen.RegisterTaskServiceServer(s, newTaskServer())
	gen.RegisterNotifierServiceServer(s, &notifierServer{})
	gen.RegisterUIServiceServer(s, newUIServer())
	return nil
}

func (p *servedPlugin) GRPCClient(
	ctx context.Context,
	broker *plugin.GRPCBroker,
	c *grpc.ClientConn,
) (interface{}, error) {
	return &notifierClient{client: gen.NewNotifierServiceClient(c)}, nil
}

// notifierClient is the host-side client stub wrapper (used by the host's
// adapter; on the plugin side it is never dispensed).
type notifierClient struct {
	client gen.NotifierServiceClient
}

func (c *notifierClient) Send(ctx context.Context, req *gen.SendRequest) (*gen.SendResponse, error) {
	return c.client.Send(ctx, req)
}

// notifierImpl holds the plugin's notifier handler, installed via
// ProvideNotifier from the plugin's Init (or before Run).
var (
	notifierMu   sync.RWMutex
	notifierImpl Notifier
)

func currentNotifier() (Notifier, bool) {
	notifierMu.RLock()
	defer notifierMu.RUnlock()
	if notifierImpl == nil {
		return nil, false
	}
	// Reject typed nils (e.g. (*MyNotifier)(nil)): they satisfy the
	// interface but dispatching to them would panic on a nil receiver.
	v := reflect.ValueOf(notifierImpl)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		if v.IsNil() {
			return nil, false
		}
	}
	return notifierImpl, true
}

// notifierServer is the plugin-side gRPC service implementation. It
// dispatches to the handler installed with ProvideNotifier; panics in the
// plugin's Send are recovered, logged and reported as status errors.
type notifierServer struct {
	gen.UnimplementedNotifierServiceServer
}

func (s *notifierServer) Send(ctx context.Context, req *gen.SendRequest) (*gen.SendResponse, error) {
	impl, ok := currentNotifier()
	if !ok {
		return &gen.SendResponse{Error: "notifier service not provided by plugin"}, nil
	}
	var sendErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				sendErr = reportPanic(r)
			}
		}()
		// Bound the plugin's Send like every other user hook: notification
		// delivery usually performs network I/O, and a handler that ignores
		// ctx cancellation must not hang the host's RPC forever. On timeout
		// the host sees a failed delivery and can retry.
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		sendErr = impl.Send(sendCtx, req.Message)
	}()
	if sendErr != nil {
		return &gen.SendResponse{Error: sendErr.Error()}, nil
	}
	return &gen.SendResponse{}, nil
}

// lifecycleServer implements the shared LifecycleService on the plugin
// side. Init dials the host-served HostService over the broker, installs it
// as the shared host client (Config/Log/ReportStatus/Data), caches the
// pushed configuration and finally calls the plugin's Init. Shutdown runs
// the plugin's optional cleanup hook and then exits the process.
type lifecycleServer struct {
	gen.UnimplementedLifecycleServiceServer
	impl   any
	broker *plugin.GRPCBroker
}

func newLifecycleServer(impl any, broker *plugin.GRPCBroker) *lifecycleServer {
	return &lifecycleServer{impl: impl, broker: broker}
}

func (s *lifecycleServer) Init(ctx context.Context, req *gen.InitRequest) (*gen.InitResponse, error) {
	if err := checkABI(req.ApiVersion); err != nil {
		return nil, err
	}
	conn, err := s.broker.Dial(req.HostBrokerId)
	if err != nil {
		return nil, err
	}
	installHostClient(conn)
	// The host pushes the current configuration with Init; cache it so
	// CurrentConfig works without any RPC. When nothing was delivered (e.g.
	// an older host), pull it once as best effort.
	if req.ConfigJson != "" {
		setConfigCache(req.ConfigJson)
	} else {
		if _, err := RefreshConfig[map[string]any](ctx); err != nil && err != ErrNoConfig {
			LogWarn("initial config fetch failed: %v", err)
		}
	}
	// First config delivery: hand it to the receivers registered so far
	// (OnConfig), then run the plugin's own Init — receivers registered
	// inside Init receive the config immediately.
	markConfigDelivered()
	if err := runConfigReceiversBounded(ctx); err != nil {
		LogError("config receiver failed: %v", err)
		return nil, status.Errorf(codes.Internal, "config receiver failed: %v", err)
	}
	if p, ok := s.impl.(Plugin); ok {
		// Same panic contract as every other user hook: a panic is reported
		// to the host (LogError + error status) instead of surfacing as an
		// opaque gRPC error.
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = reportPanic(r)
				}
			}()
			return p.Init(ctx)
		}()
		if err != nil {
			LogError("plugin init failed: %v", err)
			return nil, status.Errorf(codes.Internal, "plugin init failed: %v", err)
		}
	}
	return &gen.InitResponse{}, nil
}

// runConfigReceiversBounded runs the OnConfig receivers with an upper
// bound: the RPC ctx usually carries no deadline, and a stuck receiver must
// not hang Init/ConfigChanged forever.
func runConfigReceiversBounded(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return runConfigReceivers(ctx)
}

func (s *lifecycleServer) ConfigChanged(ctx context.Context, req *gen.ConfigChangedRequest) (*gen.ConfigChangedResponse, error) {
	setConfigCache(req.ConfigJson)
	// Typed receivers first (OnConfig), then the legacy untyped hook.
	if err := runConfigReceiversBounded(ctx); err != nil {
		LogError("config receiver failed: %v", err)
		ReportStatus(StatusError, "config receiver failed: "+err.Error())
	}
	if hook, ok := s.impl.(ConfigChangeAware); ok {
		// timedOut marks that the handler gave up waiting: the hook's
		// goroutine may outlive this RPC (it ignores ctx cancellation), so
		// it must not report stale error status afterwards and race with
		// later ConfigChanged/Shutdown reports.
		var timedOut atomic.Bool
		done := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil && !timedOut.Load() {
					reportPanic(r)
				}
				close(done)
			}()
			if err := hook.OnConfigChanged(ctx); err != nil {
				LogError("config change hook failed: %v", err)
				if !timedOut.Load() {
					ReportStatus(StatusError, "config change hook failed: "+err.Error())
				}
			}
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			timedOut.Store(true)
			LogWarn("config change hook timed out")
		}
	}
	return &gen.ConfigChangedResponse{}, nil
}

func (s *lifecycleServer) Shutdown(ctx context.Context, req *gen.ShutdownRequest) (*gen.ShutdownResponse, error) {
	runShutdownHook(ctx, s.impl)
	// Reply first, then exit so the host receives the response. The delay
	// gives gRPC time to flush it.
	go func() {
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}()
	return &gen.ShutdownResponse{}, nil
}

// runShutdownHook invokes the plugin's optional ShutdownAware hook with a
// bound so a stuck cleanup cannot prevent the process from exiting.
func runShutdownHook(ctx context.Context, impl any) {
	hook, ok := impl.(ShutdownAware)
	if !ok {
		return
	}
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				reportPanic(r)
			}
			close(done)
		}()
		if err := hook.Shutdown(ctx); err != nil {
			LogError("shutdown hook failed: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		LogWarn("shutdown hook timed out")
	}
}

// reportPanic surfaces a recovered panic to the host: error-level log with
// the stack plus an error status, and returns the error for the caller.
func reportPanic(r any) error {
	LogError("panic in plugin: %v\n%s", r, debug.Stack())
	ReportStatus(StatusError, fmt.Sprintf("panic: %v", r))
	return fmt.Errorf("panic: %v", r)
}
