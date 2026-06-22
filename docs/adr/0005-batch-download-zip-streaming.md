# ADR-0005: 批量下载支持 zip 流式打包

## 状态
已实施（2026-06-22）

## 背景
批量下载需要将多个文件打包成一个归档文件供用户下载。此前仅支持 tar.gz 和 tar.zst（基于 tar 的归档格式），不包含 zip。用户希望两种格式都支持，其中 zip 在 Windows 上兼容性更好。

## 决策
使用 Go 标准库 `archive/zip` 实现 zip 流式打包。Compressor 适配器新增 `zipArchiveWriter`：

```go
type zipArchiveWriter struct {
    zw *zip.Writer
}
```

通过 `io.Pipe` 实现流式输出：写入 local file header + 文件数据 + data descriptor，最后写入 central directory。

## 备选方案
- **仅支持 tar.gz**：最简单，已有完整支持。被否决，因为 Windows 用户需要 zip。
- **第三方库**：使用 `github.com/mholt/archiver` 等库。被否决，标准库已能满足需求，且减少依赖。
- **内存中组装再输出**：小文件集合可工作，大文件浪费内存。被否决，必须流式。

## 技术细节
Go 的 `archive/zip.Writer` 支持流式输出：
- 每次 `CreateHeader` 写 local file header 和 data descriptor marker
- 文件数据写入后自动计算 CRC-32 和压缩后大小
- `Close()` 时写入 central directory，完成 zip 规范格式

## 影响
- BatchHandler 的 `/v1/batch/download` 端点支持 `format` 参数（`zip` / `tar.gz`）
- 前端 BatchToolbar 增加格式选择下拉菜单
- Compressor 单元测试扩展了 zip 验证
