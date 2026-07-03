# 修复：root 用户拥有的目录导致应用写入失败

## 背景

Reasonix Desktop 在 macOS 上使用两个配置目录：
- **主配置目录**: `~/Library/Application Support/reasonix/` — 存储桌面项目注册、标签页状态等
- **项目元数据目录**: `<workspaceRoot>/.reasonix/` — 存储每个项目的 topic 标题、创建时间等

当这些目录被 **root** 用户创建/拥有时（例如通过 `sudo` 运行过应用），当前用户没有写入权限，导致所有持久化操作失败。

---

## 问题 1：添加项目后侧边栏不显示

### 现象
点击"添加项目"，选择文件夹后，右侧项目列表中没有出现该项目。

### 根因
`desktopConfigDir()` 返回 `~/Library/Application Support/reasonix/`，该目录由 root 拥有（`drwxr-xr-x root`），当前用户无法写入。
`saveProjectsFile()` 尝试写入 `desktop-projects.json` 时因 **Permission denied** 失败，项目注册信息从未被持久化。

### 修复

**文件**: `desktop/tabs.go` — `desktopConfigDir()`

将 `desktopConfigDir()` 从简单路径拼接改为带可写性检查的版本：

```go
// 修复前
func desktopConfigDir() string {
    dir, err := os.UserConfigDir()
    if err != nil {
        home, _ := os.UserHomeDir()
        return filepath.Join(home, ".reasonix")
    }
    return filepath.Join(dir, "reasonix")
}

// 修复后
var (
    desktopConfigDirOnce  sync.Once
    desktopConfigDirCache string
)

func desktopConfigDir() string {
    desktopConfigDirOnce.Do(func() {
        desktopConfigDirCache = resolveDesktopConfigDir()
    })
    return desktopConfigDirCache
}

func resolveDesktopConfigDir() string {
    dir, err := os.UserConfigDir()
    if err != nil { /* fallback */ }
    primary := filepath.Join(dir, "reasonix")
    // 如果主目录存在但不可写，回退到 ~/.reasonix/desktop/
    if info, err := os.Stat(primary); err == nil && info.IsDir() {
        if !isDirWritable(primary) {
            return filepath.Join(config.MemoryUserDir(), "desktop")
        }
    }
    return primary
}
```

新增 `isDirWritable()` 辅助函数：
```go
func isDirWritable(dir string) bool {
    f, err := os.CreateTemp(dir, ".writable-test-*")
    if err != nil { return false }
    name := f.Name()
    f.Close()
    os.Remove(name)
    return true
}
```

**文件**: `desktop/app.go` — `SwitchWorkspace()`

- 在 `CreateTopic` 之前先调用 `addProject()`，确保项目立即写入项目文件
- `CreateTopic` 失败时不再返回错误，记录日志 + 触发前端刷新 + 返回目录路径
- `ActivateTopic` / `OpenProjectTab` 失败时同样容错处理

---

## 问题 2：项目添加后无法创建对话（Topic）

### 现象
项目已出现在侧边栏，但无法在该项目中创建新对话/会话。

### 根因
项目目录下的 `.reasonix/` 由 root 拥有（`drwxr-xr-x root`），当前用户无法写入。
`CreateTopic()` → `setTopicTitleWithSource()` → `saveTopicTitles()` 尝试向 `<workspaceRoot>/.reasonix/desktop-topic-titles.json` 写入，因权限不足失败。

### 修复

**文件**: `desktop/tabs.go`

引入 `projectTopicMetaDir()` 函数，统一管理项目 topic 元数据的存储位置：

```go
var projectTopicMetaDirCache sync.Map

func projectTopicMetaDir(workspaceRoot string) string {
    if workspaceRoot == "" {
        return filepath.Join(desktopConfigDir(), "global")
    }
    if v, ok := projectTopicMetaDirCache.Load(workspaceRoot); ok {
        return v.(string)
    }
    primary := filepath.Join(workspaceRoot, ".reasonix")
    dir := primary
    if !isDirWritableOrCreatable(primary) {
        slug := config.WorkspaceSlug(workspaceRoot)
        dir = filepath.Join(desktopConfigDir(), "projects", slug)
    }
    projectTopicMetaDirCache.Store(workspaceRoot, dir)
    return dir
}
```

新增 `isDirWritableOrCreatable()`：
```go
func isDirWritableOrCreatable(dir string) bool {
    info, err := os.Stat(dir)
    if err != nil {
        if !errors.Is(err, os.ErrNotExist) { return false }
        return isDirWritable(filepath.Dir(dir))  // 父目录可写即可创建
    }
    if !info.IsDir() { return false }
    return isDirWritable(dir)
}
```

三个 topic 路径函数改为通过 `projectTopicMetaDir()` 解析：

```go
func topicTitlesPath(workspaceRoot string) string {
    return filepath.Join(projectTopicMetaDir(workspaceRoot), topicTitlesFile)
}
func topicTitleSourcesPath(workspaceRoot string) string {
    return filepath.Join(projectTopicMetaDir(workspaceRoot), topicTitleSourcesFile)
}
func topicCreatedAtsPath(workspaceRoot string) string {
    return filepath.Join(projectTopicMetaDir(workspaceRoot), topicCreatedAtsFile)
}
```

---

## 前端辅助修复

**文件**: `frontend/src/components/ProjectTree.tsx` — `handleAddProject`
- 将 `refresh()` 移到 `finally` 块，确保即使添加失败也会刷新项目树

**文件**: `frontend/src/lib/useController.ts` — `refreshWorkspaceState`
- 用 try/catch 包裹 `syncActiveTabFromBackend`，防止其异常阻断 `setProjectRevision` 调用

---

## 测试覆盖

**文件**: `desktop/switch_workspace_readonly_test.go`

| 测试用例 | 场景 | 验证 |
|---------|------|------|
| `TestSwitchWorkspaceWithReadOnlyProjectDir` | `.reasonix` 是文件（非目录） | 项目仍出现在侧边栏 |
| `TestSwitchWorkspaceWithUnwritableConfigDir` | 主配置目录 `~/Library/Application Support/reasonix/` 只读 | 项目回退到 `~/.reasonix/desktop/` |
| `TestCreateTopicWithUnwritableReasonixDir` | 项目 `.reasonix/` 不可写 | Topic 元数据回退到配置目录 |

**文件**: `desktop/app_test.go` — `isolateDesktopUserDirs()`
- 增加 `resetDesktopConfigDirCacheForTesting()` 和 `resetProjectTopicMetaDirCacheForTesting()` 调用

---

## 架构设计

```
desktopConfigDir()
├── 主路径: ~/Library/Application Support/reasonix/  (默认)
├── 回退路径: ~/.reasonix/desktop/                   (主路径不可写时)
│
projectTopicMetaDir(workspaceRoot)
├── 主路径: <workspaceRoot>/.reasonix/              (默认)
└── 回退路径: <desktopConfigDir()>/projects/<slug>/  (主路径不可写/不可创建时)
```

### 回退触发条件

| 场景 | `os.Stat(dir)` | `isDirWritable(dir)` | 结果 |
|------|---------------|---------------------|------|
| 目录不存在 | 返回 ErrNotExist | 检查父目录 | 父目录可写 → 主路径 |
| 目录存在且可写 | 返回 DirInfo | true | 主路径 |
| 目录存在但不可写 | 返回 DirInfo | false | **回退路径** |
| 路径是文件不是目录 | 返回 FileInfo | - | **回退路径** |

---

## 修改文件清单

| 文件 | 修改内容 |
|------|---------|
| `desktop/tabs.go` | `desktopConfigDir()` 增加可写性检查与回退；新增 `projectTopicMetaDir()`、`isDirWritable()`、`isDirWritableOrCreatable()`；topic 路径函数统一使用 `projectTopicMetaDir()` |
| `desktop/app.go` | `SwitchWorkspace` 先 `addProject` 再 `CreateTopic`；失败时容错处理 |
| `desktop/app_test.go` | `isolateDesktopUserDirs` 中重置缓存 |
| `desktop/switch_workspace_readonly_test.go` | 新增 3 个测试用例 |
| `frontend/src/components/ProjectTree.tsx` | `refresh()` 移到 `finally` 块 |
| `frontend/src/lib/useController.ts` | `refreshWorkspaceState` 增加异常捕获 |
