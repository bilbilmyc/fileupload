# COS 风格前端界面 — PRD

**Status:** Draft

**Date:** 2026-06-23

---

## Problem Statement

当前前端页面采用单列居中布局，所有功能（上传、文件浏览、批量操作）堆叠在一个垂直流中。上传面板固定在文件列表上方占用大量空间，顶部栏过于拥挤（搜索、筛选、命名空间、主题、刷新、设置共 8 个交互元素），内容区域最大宽度 1200px 在宽屏下有大量留白浪费。整体观感更像一个管理后台，而非专业的对象存储文件管理工具。

用户期望界面更接近腾讯云 COS（Cloud Object Storage）控制台风格：左侧导航、操作工具栏、全宽布局、上传进度不阻塞浏览。

---

## Solution

将现有单列居中布局改造为 COS 风格的三栏布局（侧边栏 + 主内容区 + 可选详情面板），主要变更：

1. 添加 **Layout.Sider 侧边导航栏**，承载品牌标识、导航菜单、命名空间切换
2. **精简顶部栏**，仅保留搜索+筛选+刷新，其余功能移入侧边栏
3. 增加 **操作按钮工具栏**（上传、新建文件夹、下载、删除），固定显示在文件表格上方
4. **上传移出主内容流**，改为底部浮动进度条或触发式模态框
5. **全宽布局**，去掉 `max-width` 限制
6. 可选：**文件详情右侧面板**，点击文件显示属性

改造以**增量方式**进行，每一阶段独立可部署、可测试。

---

## User Stories

1. As a **user browsing files**, I want a **left sidebar navigation**, so that I can quickly switch between namespaces and access system functions without scrolling back to the top.

2. As a **user managing files**, I want a **clean top bar with only search and type filter**, so that the interface feels less cluttered and focused on the file content.

3. As a **user uploading files**, I want upload progress shown in a **bottom bar or drawer**, so that I can continue browsing files while uploads run in the background.

4. As a **user managing a directory**, I want a **persistent action toolbar** (Upload, New Folder, Download, Delete), so that I can access common operations with one click regardless of selection state.

5. As a **user on a wide screen**, I want **full-width file listing**, so that I can see more files without horizontal scrolling.

6. As a **user previewing file details**, I want a **right-side properties panel**, so that I can see metadata (size, type, tags, hash) without opening a modal.

7. As a **developer extending the UI**, I want the **sidebar to have collapsible navigation groups**, so that future features (admin dashboard, logs, settings) can be added without redesigning the layout.

8. As a **mobile user**, I want the **sidebar to collapse into a drawer**, so that the file listing remains usable on narrow screens.

---

## Implementation Decisions

### Decision 1: Layout Architecture

The current Ant Design `Layout` wrapper in `App.tsx` will be extended with `Layout.Sider`. The content structure becomes:

```
Layout
├─ Layout.Sider (collapsible, 200px)
│  ├─ Logo / Brand
│  ├─ Nav Menu (Files, Console, Logs, Settings)
│  └─ Namespace selector
├─ Layout
│  ├─ TopBar (slim: search + filter + refresh)
│  ├─ Layout.Content (full-width, no max-width)
│  │  ├─ Breadcrumb
│  │  ├─ ActionToolbar
│  │  ├─ FileTable
│  │  ├─ BatchToolbar (when items selected)
│  │  └─ UploadProgressBar (floating bottom)
│  └─ (optional) Layout.Sider (right, for properties)
└─ Modals / Drawers
```

The sidebar uses Ant Design `Menu` component with `selectedKeys` tied to current route.

### Decision 2: Sidebar Menu Structure

Default navigation items:

| Icon | Label | Route |
|------|-------|-------|
| 📁 | 文件管理 | `/` |
| 📊 | 控制台 | `/admin` |
| 📋 | 操作日志 | `/logs` |
| ⚙️ | 设置 | `/settings` |

At the bottom of the sidebar: namespace selector + theme toggle.

### Decision 3: Upload Progress Component

The upload lifecycle moves from an inline `UploadPanel` to a **floating bottom bar**:

- When no upload is active: hidden (0 height)
- When upload starts: slides up with a minimal progress row per task
- User can expand/collapse to see detailed progress
- Upload task list persists across navigation via context hoisting

The `useUpload` hook logic remains the same; the rendering moves from `Files.tsx` into a new `UploadProgressBar` component at the `App.tsx` level.

### Decision 4: Action Toolbar

New `ActionToolbar` component positioned between breadcrumb and file table:

```
[+ 上传] [📁 新建文件夹] [↓ 下载] [✕ 删除] [↻ 刷新]
```

- Buttons are disabled when no file is selected (download/delete)
- Upload triggers a file picker or a drop zone overlay
- New Folder triggers an inline input to create a directory
- All handlers already exist in `useFileOperations`

### Decision 5: State Management for Upload

Upload state (`useUpload`) needs to be hoisted from `Files.tsx` to a higher level or a context so the bottom progress bar can access it from any page. An `UploadContext` will wrap the app and provide:

```typescript
interface UploadContextType {
  uploadTasks: UploadTask[]
  hasActiveUploads: boolean
  customRequest: (file: File) => void
  clearDoneTasks: () => void
}
```

### Decision 6: Routing Expansion

Current routes (`/`, `/login`) expand to:

| Route | Component |
|-------|-----------|
| `/login` | Login |
| `/` | Files (existing) |
| `/admin` | AdminDashboard (new, placeholder) |
| `/logs` | AuditLogPage (new, placeholder) |
| `/settings` | SettingsPage (new, placeholder) |

This enables the sidebar to have real navigation targets. The new pages can start as stubs and be filled later.

### Decision 7: Full-width Layout

Remove `maxWidth: 1200` and `margin: '0 auto'` from the `<Content>` style. Let content fill available space with `flex: 1` and reasonable padding.

---

## Testing Decisions

### What makes a good test

Each test should exercise behavior through the narrowest public interface possible. For frontend changes:

- **Layout components** (Sidebar, TopBar, Toolbar) are tested by rendering with mock props and asserting DOM structure
- **Upload progress** behavior (collapsing, task display) is tested by providing mock upload state
- **Sidebar navigation** is tested by simulating menu clicks and asserting route changes
- **Existing backend behavior** must not change — all existing Go tests must pass

### Test modules

| Module | Type | Prior art |
|--------|------|-----------|
| `web/src/components/Sidebar.tsx` | Render test | New |
| `web/src/components/ActionToolbar.tsx` | Render test | New |
| `web/src/components/UploadProgressBar.tsx` | Render + state test | New |
| `web/src/context/UploadContext.tsx` | Integration test | Pattern from AuthContext |
| Backend unchanged | — | Existing tests must pass |

### Seams

Testing happens at the **component props seam**, not through end-to-end HTTP. The sidebar receives `namespace`, `route`, and `onNavigate` as props — tests provide these directly without mounting the full app.

---

## Out of Scope

- **Bucket abstraction** — bucket listing page, bucket-level permissions, bucket settings. The current namespace model remains unchanged.
- **COS-style drag-and-drop overlay** — drag-and-drop file upload across the entire page (vs. a drop zone) is not implemented in this phase.
- **File versioning UI** — COS bucket versioning is not exposed.
- **Presigned URL / share link generation** — the backend has share functionality but frontend sharing UI is unchanged.
- **Mobile responsive optimization** — sidebar collapses to drawer but full mobile navigation redesign is deferred.
- **Backend changes** — all changes are frontend-only; no Go code is modified.

---

## Further Notes

- **Phase 1** (Layout restructure): Sidebar + slim TopBar + full-width + ActionToolbar. This is purely rearranging existing components and can be done independently.
- **Phase 2** (Upload relocation): UploadContext + UploadProgressBar. Requires hoisting state, so it's more invasive.
- **Phase 3** (Right properties panel): File detail side panel. Low priority, can be deferred.
- **Phase 4** (New routes/pages): Admin dashboard, audit log, settings page stubs. Enable sidebar navigation targets.
- ADR-0001 through ADR-0005 are not contradicted by any of the proposed changes.
