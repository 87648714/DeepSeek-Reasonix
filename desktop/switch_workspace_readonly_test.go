package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSwitchWorkspaceWithReadOnlyProjectDir verifies that SwitchWorkspace
// still registers the project in the sidebar when CreateTopic fails because
// the workspace directory cannot hold a .reasonix/ sub-directory (e.g. a
// file named .reasonix already exists, or the directory is read-only).
func TestSwitchWorkspaceWithReadOnlyProjectDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	// Create a *file* named .reasonix so that os.MkdirAll(.reasonix) inside
	// saveTopicTitles fails — this simulates a read-only or hostile workspace.
	if err := os.WriteFile(filepath.Join(projectRoot, ".reasonix"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	got, err := app.SwitchWorkspace(projectRoot)
	if err != nil {
		t.Fatalf("SwitchWorkspace returned error: %v", err)
	}
	if got != projectRoot {
		t.Fatalf("SwitchWorkspace root = %q, want %q", got, projectRoot)
	}

	// The project must appear in the sidebar even though CreateTopic failed.
	nodes := app.ListProjectTree()
	found := false
	for _, n := range nodes {
		if n.Root == projectRoot {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("project %q not found in tree: %+v", projectRoot, nodes)
	}
}

// TestSwitchWorkspaceWithUnwritableConfigDir verifies that the project appears
// in the sidebar even when the primary desktop config directory (returned by
// os.UserConfigDir()) is not writable — e.g. it was created by root.
func TestSwitchWorkspaceWithUnwritableConfigDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	// Create the primary config dir and make it read-only to simulate a
	// root-owned directory that the current user cannot write to.
	primary := desktopConfigDir()
	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(primary, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(primary, 0o755) })
	// Force re-resolution so the writability check kicks in.
	resetDesktopConfigDirCacheForTesting()

	projectRoot := t.TempDir()
	app := NewApp()
	got, err := app.SwitchWorkspace(projectRoot)
	if err != nil {
		t.Fatalf("SwitchWorkspace returned error: %v", err)
	}
	if got != projectRoot {
		t.Fatalf("SwitchWorkspace root = %q, want %q", got, projectRoot)
	}

	// The project must appear in the sidebar even though the primary config
	// dir is not writable (the fallback dir should be used).
	nodes := app.ListProjectTree()
	found := false
	for _, n := range nodes {
		if n.Root == projectRoot {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("project %q not found in tree: %+v", projectRoot, nodes)
	}
}

// TestCreateTopicWithUnwritableReasonixDir verifies that CreateTopic succeeds
// even when the project's .reasonix/ directory is not writable (e.g. owned by
// root). Topic metadata should fall back to the desktop config directory.
func TestCreateTopicWithUnwritableReasonixDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	// Create .reasonix as a read-only directory owned by the test user but
	// not writable — simulating a root-owned .reasonix that the app can't
	// write into.
	reasonixDir := filepath.Join(projectRoot, ".reasonix")
	if err := os.MkdirAll(reasonixDir, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(reasonixDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(reasonixDir, 0o755) })

	// Make sure the cache picks up the read-only state.
	resetProjectTopicMetaDirCacheForTesting()

	app := NewApp()
	// CreateTopic should succeed by falling back to the desktop config dir.
	topic, err := app.CreateTopic("project", projectRoot, "Test topic")
	if err != nil {
		t.Fatalf("CreateTopic returned error: %v", err)
	}
	if topic.ID == "" {
		t.Fatal("CreateTopic returned empty topic ID")
	}

	// Verify topic metadata was written to the fallback location, not .reasonix.
	primaryPath := filepath.Join(reasonixDir, topicTitlesFile)
	if _, err := os.Stat(primaryPath); !os.IsNotExist(err) {
		t.Fatalf("topic titles should NOT be in .reasonix (read-only), but got err=%v", err)
	}

	// The topic should appear in ListProjectTree.
	_ = addProject(projectRoot, "")
	_ = prependTopicInProjectsFile(projectRoot, topic.ID, true)
	nodes := app.ListProjectTree()
	found := false
	for _, n := range nodes {
		if n.Root == projectRoot {
			for _, child := range n.Children {
				if child.TopicID == topic.ID {
					found = true
					if child.Label != "Test topic" {
						t.Fatalf("topic label = %q, want %q", child.Label, "Test topic")
					}
					break
				}
			}
		}
	}
	if !found {
		t.Fatalf("topic %q not found in project tree", topic.ID)
	}
}
