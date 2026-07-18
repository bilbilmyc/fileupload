# Web 测试覆盖率报告（v0.9.0）

## 跑法

```bash
pnpm --dir web test:coverage
```

输出文本报告到 stdout，HTML 报告到 `web/coverage/index.html`。

## 当前覆盖率（v0.9.0 snapshot）

```
% Coverage report from v8
-------------------|---------|----------|---------|---------|-------------------
File               | % Stmts | % Branch | % Funcs | % Lines | Uncovered Line #s
-------------------|---------|----------|---------|---------|-------------------
All files          |   46.24 |    44.22 |    39.7 |   47.26 |
 api               |    44.3 |     65.9 |   22.58 |   47.22 |
  client.ts        |   25.92 |    45.45 |    9.09 |   29.16 | ...4,59-63,84-214
  sdk.ts           |     100 |    91.66 |     100 |     100 | 46
  sdkAdapter.ts    |      60 |       80 |   33.33 |      60 | 21-36
 components        |   52.63 |    46.73 |   49.12 |   52.94 |
  ActionToolbar.tsx|      50 |      100 |      50 |      50 | 27-28
  DirectoryPicker  |      50 |    27.77 |   42.85 |   50.98 | ...85,105-108,127
  FileTable.tsx    |   53.57 |    46.66 |   54.16 |   54.16 | ...48,258,262-269
  Sidebar.tsx      |      80 |       75 |    33.33 |      80 | 54-66
  UploadPanel.tsx  |   92.85 |    86.84 |     100 |   92.3 | 32
  UploadProgressBar|   14.28 |     7.4 |    12.5 |   10.52 | 10-90
 context           |   38.88 |       20 |   39.58 |   39.51 |
  AuthContext.tsx  |      50 |       25 |      60 |   45.94 | ...46-50,54-55,78
  ThemeContext.tsx |   71.42 |    43.75 |   54.54 |      72 | 29,38-41,45,55-56
  UploadContext.tsx|   21.05 |     6.25 |   25.92 |   22.58 | 42-112,129-138
-------------------|---------|----------|---------|---------|-------------------

Statements   : 46.24% ( 191/413 )
Branches     : 44.22% ( 134/303 )
Functions    : 39.7 ( 54/136 )
Lines        : 47.26% ( 173/366 )
```

## 重点模块解读

| 模块 | 覆盖 | 说明 |
|---|---|---|
| **`sdk.ts`** | **100%** | SDK wrapper 完整覆盖（lazy-init / token / refresh / method 转发） |
| **`UploadPanel.tsx`** | 92.85% | v0.7.0 加测试后提升 |
| `Sidebar.tsx` | 80% | 列表渲染覆盖，未触发 onClick |
| `ThemeContext.tsx` | 71.42% | 切换主题路径覆盖 |
| `sdkAdapter.ts` | 60% | listFilesSDK 已覆盖，batchDownload 未测全（mock fetch） |

## 覆盖率低的原因分析

| 文件 | 覆盖率低原因 | 是否需要补救 |
|---|---|---|
| `client.ts` (25.92%) | 大量未迁移到 SDK 的 axios 函数（保留兼容） | **否** — 这些函数已被 sdkAdapter 替代，未来删除 |
| `UploadContext.tsx` (21.05%) | 内部 hook 逻辑（上传状态机）需重构为可测形式 | **可** — 加 useUpload hook 测试 |
| `UploadProgressBar.tsx` (14.28%) | 进度条 props 驱动组件 | **否** — 视觉组件，价值低 |
| `DirectoryPicker.tsx` (50%) | Tree 加载依赖后端列表 | **可** — 加 mock 测试 |

## 改进方向（未来 sprint）

1. **UploadContext 重构**：把 useUpload 拆出独立模块 + 加 vitest
2. **client.ts 清理**：删除完全迁移到 SDK 的函数（删前先 deprecate 一版本）
3. **目标覆盖率**：核心 SDK 路径 ≥ 80%（当前 sdk.ts 已 100%）

## 排除规则

当前 `vitest.config.ts` 排除 `e2e/**`（由 Playwright 单独跑）。生产代码不过滤任何文件。

如需排除 vendor 生成的代码或类型定义：

```ts
// vitest.config.ts
test: {
  coverage: {
    exclude: ['**/*.d.ts', '**/node_modules/**', 'web/test-setup.ts']
  }
}
```