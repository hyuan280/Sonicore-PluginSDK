// Package main is the reference "plain" demo plugin: it implements no
// extension-point service (manifest declares no services), only the minimal
// pluginsdk.Plugin interface plus the optional lifecycle hooks. It serves
// as the reference for the UI assembly APIs (OnForm/OnPage, the Go
// equivalent of an in-process plugin's get_form/get_page) and for the
// scheduled-task API — including fields that are deliberately not used by
// the plugin itself, just to show every available option.
package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hyuan280/Sonicore-PluginSDK/go"
)

// demoConfig is declared in manifest.toml ([plugin.config]).
type demoConfig struct {
	Greeting string `json:"greeting"`
}

type demo struct{}

// Init implements pluginsdk.Plugin. The SDK calls it after the host
// connection and the configuration cache are ready. Here the plugin
// declares its config receiver, its config form, its data page and a
// scheduled task.
func (d *demo) Init(ctx context.Context) error {
	pluginsdk.LogInfo("demo initialized: plugin=%s dir=%s", pluginsdk.PluginID(), pluginsdk.PluginDir())
	pluginsdk.ReportStatus(pluginsdk.StatusOK, "")
	// Typed config receiver: the SDK hands over every config delivery
	// (Init 首次下发 + 管理页保存后的热推送). The delivered JSON contains
	// whatever was stored — manifest fields AND form-only fields the admin
	// saved in the UI.
	if err := pluginsdk.OnConfig(d.setConfig); err != nil {
		return err
	}
	if err := pluginsdk.OnForm(d.form); err != nil {
		return err
	}
	if err := pluginsdk.OnPage(d.page); err != nil {
		return err
	}
	// Declarative schedule (interval form; cron form is spec.Cron = "0 * * * *").
	return pluginsdk.RegisterSchedule(ctx, pluginsdk.ScheduleSpec{
		ID:       "heartbeat",
		Name:     "心跳日志",
		Interval: "10s",
	}, func(ctx context.Context) error {
		ticks := 0
		if v, err := pluginsdk.Data[int](ctx, "ticks"); err == nil {
			ticks = v
		}
		ticks++
		if err := pluginsdk.SetData(ctx, "ticks", ticks); err != nil {
			return err
		}
		cfg, err := pluginsdk.CurrentConfig[demoConfig]()
		if err != nil {
			cfg = demoConfig{}
		}
		pluginsdk.LogInfo("demo heartbeat tick #%d, greeting=%q", ticks, cfg.Greeting)
		return nil
	})
}

// form is the config form assembly (get_form equivalent) written with the
// Vuetify vocabulary — component names straight from the Vuetify
// documentation. Field props follow the Vuetify habits (model/label/...);
// most keys below are reference examples and are not used by this plugin
// (they are still stored by the host).
func (d *demo) form(ctx context.Context) pluginsdk.Component {
	return pluginsdk.Component{
		Component: "VForm",
		Content: []pluginsdk.Component{
			{
				Component: "VRow",
				Content: []pluginsdk.Component{
					{
						Component: "VCol",
						Props:     map[string]any{"cols": 12},
						Content: []pluginsdk.Component{
							{
								Component: "VTextField",
								Props: map[string]any{
									"model":       "greeting",
									"label":       "问候语",
									"placeholder": "Hello",
									"help":        "插件实际使用的配置项",
								},
							},
							{
								Component: "VTextarea",
								Props:     map[string]any{"model": "notes", "label": "备注（参考：VTextarea）"},
							},
							{
								Component: "VTextField",
								Props: map[string]any{
									"model": "retry_count",
									"label": "重试次数（参考：type=number）",
									"type":  "number",
								},
							},
						},
					},
					{
						Component: "VCol",
						Props:     map[string]any{"cols": 4},
						Content: []pluginsdk.Component{
							{
								Component: "VTextField",
								Props:     map[string]any{"model": "token", "label": "令牌（参考：密码类字段）"},
							},
							{
								Component: "VSwitch",
								Props:     map[string]any{"model": "verbose", "label": "详细日志"},
							},
							{
								Component: "VCheckbox",
								Props:     map[string]any{"model": "auto_reload", "label": "自动重载（参考）"},
							},
							{
								Component: "VSelect",
								Props: map[string]any{
									"model": "notify_level",
									"label": "通知级别（参考：单选）",
									"items": []map[string]string{
										{"title": "调试", "value": "debug"},
										{"title": "信息", "value": "info"},
										{"title": "警告", "value": "warn"},
									},
								},
							},
							{
								Component: "VSelect",
								Props: map[string]any{
									"model":    "tags",
									"label":    "标签（参考：多选）",
									"multiple": true,
									"items": []map[string]string{
										{"title": "标签A", "value": "a"},
										{"title": "标签B", "value": "b"},
										{"title": "标签C", "value": "c"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// page is the data page assembly (get_page equivalent). The builder runs
// per request, so the content always reflects the current plugin state.
func (d *demo) page(ctx context.Context) pluginsdk.Component {
	ticks := 0
	if v, err := pluginsdk.Data[int](ctx, "ticks"); err == nil {
		ticks = v
	}
	cfg, err := pluginsdk.CurrentConfig[demoConfig]()
	if err != nil {
		cfg = demoConfig{}
	}
	return pluginsdk.Component{
		Component: "VRow",
		Content: []pluginsdk.Component{
			{
				Component: "VCol",
				Props:     map[string]any{"cols": 12},
				Content: []pluginsdk.Component{
					{
						Component: "VAlert",
						Props: map[string]any{
							"type": "info",
							"text": "本页数据由插件 OnPage 实时生成（参考示例）",
						},
					},
				},
			},
			{
				Component: "VCol",
				Props:     map[string]any{"cols": 6},
				Content: []pluginsdk.Component{
					{
						Component: "VCard",
						Content: []pluginsdk.Component{
							{Component: "VCardTitle", Props: map[string]any{"text": "心跳"}},
							{Component: "VCardText", Props: map[string]any{
								"text": fmt.Sprintf("心跳次数：%d", ticks),
							}},
							{Component: "VCardText", Props: map[string]any{
								"text": fmt.Sprintf("问候语：%s", cfg.Greeting),
							}},
						},
					},
				},
			},
			{
				Component: "VCol",
				Props:     map[string]any{"cols": 6},
				Content: []pluginsdk.Component{
					{
						Component: "VCard",
						Content: []pluginsdk.Component{
							{Component: "VCardTitle", Props: map[string]any{"text": "当前配置"}},
							{Component: "VTable", Props: map[string]any{
								"columns": []string{"字段", "值"},
								"rows": [][]string{
									{"greeting", cfg.Greeting},
									{"ticks", fmt.Sprint(ticks)},
								},
							}},
						},
					},
					{
						Component: "VCard",
						Content: []pluginsdk.Component{
							{Component: "VCardTitle", Props: map[string]any{"text": "参考"}},
							{Component: "VCardText", Props: map[string]any{
								"text": "该卡片为纯文本参考示例，未展示任何插件数据",
							}},
						},
					},
				},
			},
		},
	}
}

// setConfig is the typed configuration receiver: the SDK calls it at Init
// and on every configuration change with the decoded values. demoConfig
// only declares the keys the plugin reads; keys that exist only in the
// form (notes/token/...) are delivered too and can be read via
// CurrentConfig — here the receiver just logs them for demonstration.
func (d *demo) setConfig(ctx context.Context, cfg demoConfig) error {
	pluginsdk.LogInfo("config received: greeting=%q", cfg.Greeting)
	if all, err := pluginsdk.CurrentConfig[map[string]any](); err == nil {
		// One multi-line log record: every config key on its own line.
		// The host escapes the newlines into a single file line, so the
		// whole dump arrives as one record in the log viewer.
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("config dump:")
		for _, k := range keys {
			fmt.Fprintf(&b, "\n  %s = %v", k, all[k])
		}
		pluginsdk.LogInfo("%s", b.String())

		for _, k := range []string{"notes", "token", "retry_count", "verbose", "notify_level", "tags", "auto_reload"} {
			if v, ok := all[k]; ok {
				pluginsdk.LogDebug("  %s = %v", k, v)
			}
		}
	}
	return nil
}

// Shutdown implements the optional ShutdownAware hook.
func (d *demo) Shutdown(ctx context.Context) error {
	pluginsdk.LogInfo("demo shutting down, bye")
	return nil
}

func main() {
	pluginsdk.Run("demo", &demo{})
}
