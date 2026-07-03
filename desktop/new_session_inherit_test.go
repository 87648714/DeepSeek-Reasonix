package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestEnsureBlankTabInheritsActiveTabLocalSettings(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"),
		[]byte(""), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	effort := "max"
	app := NewApp()
	src := &WorkspaceTab{
		ID:               "src",
		Scope:            "project",
		WorkspaceRoot:    workspace,
		TopicID:          "topic_src",
		SessionPath:      filepath.Join(workspace, "src.jsonl"), // non-empty so src isn't reused as the blank tab
		model:            "inherit/model",
		effort:           &effort,
		tokenMode:        "economy",
		mode:             "plan",
		toolApprovalMode: control.ToolApprovalYolo,
		disabledMCP:      map[string]ServerView{"srv-x": {}},
		mcpOrder:         []string{"srv-x", "srv-y"},
	}
	src.sink = &tabEventSink{tabID: "src", app: app}
	app.tabs["src"] = src
	app.tabOrder = []string{"src"}
	app.activeTabID = "src"

	meta, err := app.EnsureBlankTab("project", workspace)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.ID == "" || meta.ID == "src" {
		t.Fatalf("expected a fresh tab, got meta.ID=%q", meta.ID)
	}

	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.model != "inherit/model" {
		t.Fatalf("model = %q, want inherited \"inherit/model\"", created.model)
	}
	if created.effort == nil || *created.effort != "max" {
		t.Fatalf("effort = %v, want inherited \"max\"", created.effort)
	}
	if created.tokenMode != "economy" {
		t.Fatalf("tokenMode = %q, want inherited \"economy\"", created.tokenMode)
	}
	if created.toolApprovalMode != control.ToolApprovalAsk {
		t.Fatalf("toolApprovalMode = %q, want global default ask", created.toolApprovalMode)
	}
	if created.mode != "normal" {
		t.Fatalf("mode = %q, want global default normal", created.mode)
	}
	if _, ok := created.disabledMCP["srv-x"]; !ok {
		t.Fatalf("disabledMCP = %v, want inherited key \"srv-x\"", created.disabledMCP)
	}
	if len(created.mcpOrder) != 2 || created.mcpOrder[0] != "srv-x" {
		t.Fatalf("mcpOrder = %v, want inherited [srv-x srv-y]", created.mcpOrder)
	}
}

func TestEnsureBlankTabInheritsActiveTabModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"),
		[]byte(""), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	// Config default is deepseek-pro, but the active tab uses a different model.
	// The new session should inherit the active tab's model, not the config default.
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetDefaultModel("deepseek-pro/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	app := NewApp()
	src := &WorkspaceTab{
		ID:            "src",
		Scope:         "project",
		WorkspaceRoot: workspace,
		TopicID:       "topic_src",
		SessionPath:   filepath.Join(workspace, "src.jsonl"),
		model:         "deepseek-flash/deepseek-v4-flash",
		disabledMCP:   map[string]ServerView{},
	}
	src.sink = &tabEventSink{tabID: "src", app: app}
	app.tabs["src"] = src
	app.tabOrder = []string{"src"}
	app.activeTabID = "src"

	meta, err := app.EnsureBlankTab("project", workspace)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.model != "deepseek-flash/deepseek-v4-flash" {
		t.Fatalf("new tab model = %q, want inherited active tab model %q", created.model, "deepseek-flash/deepseek-v4-flash")
	}
	if src.model != "deepseek-flash/deepseek-v4-flash" {
		t.Fatalf("existing tab should not be overwritten, got model=%q", src.model)
	}
	if !strings.Contains(filepath.Base(created.SessionPath), "deepseek-v4-flash") {
		t.Fatalf("new session path = %q, want filename seeded by inherited model", created.SessionPath)
	}
}

func TestEnsureBlankTabFallsBackToConfigDefaultWhenActiveTabHasNoModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"),
		[]byte(""), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetDefaultModel("deepseek-pro/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
	if err := cfg.SetDesktopDefaultToolApprovalMode(control.ToolApprovalAuto); err != nil {
		t.Fatalf("SetDesktopDefaultToolApprovalMode: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	app := NewApp()
	src := &WorkspaceTab{
		ID:               "src",
		Scope:            "project",
		WorkspaceRoot:    workspace,
		TopicID:          "topic_src",
		SessionPath:      filepath.Join(workspace, "src.jsonl"),
		model:            "", // no model on active tab
		mode:             "plan",
		toolApprovalMode: control.ToolApprovalAsk,
		disabledMCP:      map[string]ServerView{},
	}
	src.sink = &tabEventSink{tabID: "src", app: app}
	app.tabs["src"] = src
	app.tabOrder = []string{"src"}
	app.activeTabID = "src"

	meta, err := app.EnsureBlankTab("project", workspace)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.model != "deepseek-pro/deepseek-v4-pro" {
		t.Fatalf("new tab model = %q, want global default model (active tab has none)", created.model)
	}
	if created.toolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("new tab toolApprovalMode = %q, want global default auto", created.toolApprovalMode)
	}
	if src.toolApprovalMode != control.ToolApprovalAsk {
		t.Fatalf("existing tab should not be overwritten, got approval=%q", src.toolApprovalMode)
	}
	if !strings.Contains(filepath.Base(created.SessionPath), "deepseek-v4-pro") {
		t.Fatalf("new session path = %q, want filename seeded by global default model", created.SessionPath)
	}
}
