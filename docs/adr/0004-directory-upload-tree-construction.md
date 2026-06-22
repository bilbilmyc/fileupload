# ADR-0004: 目录上传目录树自动构建

## 状态
已实施（2026-06-22）

## 背景
目录上传时，前端提交的 `DirEntry.Path` 包含子目录前缀（如 `subdir/file.txt`），但后端 SubmitDir 将所有文件直接挂在根目录下，不创建中间目录节点。导致目录结构扁平化。

## 决策
SubmitDir 解析所有 entry 的 Path 字段，自动创建中间目录节点：

1. 遍历所有 entry.Path，收集不重复的目录前缀（每遇到 `/` 切一次）
2. 按字典序排序，确保父目录先于子目录创建
3. 为每个目录前缀创建 is_dir=true 的 FileMetadata 节点
4. 将文件挂载到对应的父目录节点下

例如 `subdir/nested/file.txt` → 自动创建 `subdir/` 和 `subdir/nested/` 目录节点。

## 备选方案
- **前端传入目录结构**：前端先构建好目录节点树再提交。被否决，因为前端当前不持有目录结构信息（webkitRelativePath 是扁平的），且增加了前端复杂度。
- **后端递归分析路径**：当前方案。

## 影响
- 目录上传后的 ListChildren 返回正确的层级结构
- DownloadDir 打包时目录结构正确
- E2E 测试和单元测试需要更新以反映新行为
- 历史平铺数据不受影响（SubmitDir 只影响新上传）
