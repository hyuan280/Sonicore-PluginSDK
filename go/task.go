package pluginsdk

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"
)

// ScheduleSpec declares a scheduled task (mirrors in-process systems where
// plugins declare services like "run every hour").
type ScheduleSpec struct {
	// ID identifies the task within the plugin (used in logs and when the
	// host calls back).
	ID string
	// Name is a human-readable description.
	Name string
	// Cron is a standard 5-field cron expression, e.g. "0 * * * *".
	Cron string
	// Interval is a duration string, e.g. "1h30m" or "10s". Exactly one of
	// Cron/Interval must be set.
	Interval string
}

// ErrNoHost is returned by RegisterSchedule before the host connection is
// established (call it from Init).
var ErrNoHost = ErrHostUnavailable

var (
	tasksMu sync.RWMutex
	tasks   = map[string]func(context.Context) error{}
)

// RegisterSchedule declares a scheduled task to the host. Call it from
// Init: the host scheduler calls fn (in this process) whenever the trigger
// fires. fn runs with panic recovery; its error is logged and reported as a
// status error.
func RegisterSchedule(ctx context.Context, spec ScheduleSpec, fn func(context.Context) error) error {
	if fn == nil {
		return errors.New("pluginsdk: schedule function is nil")
	}
	if spec.ID == "" {
		return errors.New("pluginsdk: schedule requires an id")
	}
	if spec.Cron == "" && spec.Interval == "" {
		return errors.New("pluginsdk: schedule requires cron or interval")
	}
	if spec.Cron != "" && spec.Interval != "" {
		return errors.New("pluginsdk: schedule requires exactly one of cron/interval")
	}
	client, err := currentHostClient()
	if err != nil {
		return err
	}
	// Register the local dispatch first: the host may fire the very first
	// trigger (1s interval, already-due cron) right after the RPC returns,
	// and RunTask must already find the task. Roll back on remote failure.
	tasksMu.Lock()
	if _, exists := tasks[spec.ID]; exists {
		tasksMu.Unlock()
		return fmt.Errorf("pluginsdk: schedule %q is already registered", spec.ID)
	}
	tasks[spec.ID] = fn
	tasksMu.Unlock()
	if _, err := client.RegisterSchedule(ctx, &gen.RegisterScheduleRequest{
		PluginId: PluginID(),
		Schedule: &gen.ScheduleSpec{
			Id:       spec.ID,
			Name:     spec.Name,
			Cron:     spec.Cron,
			Interval: spec.Interval,
		},
	}); err != nil {
		tasksMu.Lock()
		delete(tasks, spec.ID)
		tasksMu.Unlock()
		return fmt.Errorf("pluginsdk: register schedule %q: %w", spec.ID, err)
	}
	return nil
}

// taskServer implements the plugin-side TaskService: the host scheduler
// calls RunTask and the SDK dispatches to the function registered via
// RegisterSchedule.
type taskServer struct {
	gen.UnimplementedTaskServiceServer
}

func newTaskServer() *taskServer { return &taskServer{} }

func (s *taskServer) RunTask(ctx context.Context, req *gen.RunTaskRequest) (*gen.RunTaskResponse, error) {
	tasksMu.RLock()
	fn, ok := tasks[req.TaskId]
	tasksMu.RUnlock()
	if !ok {
		return &gen.RunTaskResponse{Error: "unknown task " + req.TaskId}, nil
	}
	var runErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				runErr = reportPanic(r)
			}
		}()
		runErr = fn(ctx)
	}()
	if runErr != nil {
		return &gen.RunTaskResponse{Error: runErr.Error()}, nil
	}
	return &gen.RunTaskResponse{}, nil
}
