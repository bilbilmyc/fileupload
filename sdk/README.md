# fileupload SDK

多语言 SDK，方便集成 fileupload 文件上传下载服务。

## Go SDK

```go
import "github.com/bilbilmyc/fileupload/sdk/go/fileupload"

client := fileupload.NewClient("http://localhost:8080")
client.SetToken("your-jwt-token")

// 上传文件
info, err := client.Upload(ctx, "large-file.dat",
    fileupload.WithConcurrency(8),
    fileupload.WithCompression("zstd"),
)

// 下载文件
resp, err := client.Download(ctx, info.FileID, "")
defer resp.Body.Close()
io.Copy(outputFile, resp.Body)

// 列目录
result, err := client.List(ctx, "/")

// 删除
err = client.Delete(ctx, info.FileID)
```

### 安装

```bash
go get github.com/bilbilmyc/fileupload/sdk/go/fileupload
```

### 文档

- `client.Upload(ctx, path, opts...)` — 上传文件，支持选项
- `client.UploadReader(ctx, r, size, name, opts...)` — 从 Reader 上传
- `client.UploadDir(ctx, dirPath, opts...)` — 上传目录
- `client.Download(ctx, fileID, rangeStr)` — 下载文件
- `client.DownloadDir(ctx, dirID, format)` — 下载目录打包
- `client.List(ctx, parentID)` — 列目录
- `client.Stat(ctx, id)` — 查询元信息
- `client.Delete(ctx, id)` / `client.DeleteDir(ctx, id)` — 删除
- `client.BatchDelete(ctx, ids)` — 批量删除
- `client.Scan(ctx)` — 触发一致性巡检

选项函数：

| 函数 | 说明 |
|------|------|
| `WithChunkSize(n)` | 分片大小（字节） |
| `WithConcurrency(n)` | 并发数 |
| `WithCompression(f)` | 压缩方式 ("none" / "zstd") |
| `WithFileName(n)` | 服务端文件名 |

---

## TypeScript SDK

```typescript
import { FileuploadClient } from '@fileupload/sdk'

const client = new FileuploadClient({
  endpoint: 'http://localhost:8080',
  token: 'your-jwt-token',
})

// 上传文件
const file = await client.upload(fileBlob, 'photo.jpg')

// 列目录
const list = await client.list('/')

// 下载
const blob = await client.download(file.file_id)
```

### 安装

```bash
npm install @fileupload/sdk
```

### React Hooks

```tsx
import { useFileList, useFileUpload } from '@fileupload/sdk'

function FileBrowser() {
  const { files, loading, navigateToDir } = useFileList()
  const { upload, uploading, progress } = useFileUpload()

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) {
      await upload(file)
      // 上传完成
    }
  }

  return (
    <div>
      {loading ? <p>加载中...</p> : files.map(f => <div key={f.file_id}>{f.name}</div>)}
      <input type="file" onChange={handleUpload} />
      {uploading && <p>上传进度: {progress}%</p>}
    </div>
  )
}
```

### API

| 方法 | 说明 |
|------|------|
| `client.upload(file, name?, opts?)` | 上传文件 |
| `client.download(fileId)` | 下载文件（返回 Blob） |
| `client.list(parent?)` | 列目录 |
| `client.stat(id)` | 查询元信息 |
| `client.delete(id)` | 删除文件 |
| `client.deleteDir(id)` | 删除目录 |
| `client.batchDelete(ids)` | 批量删除 |
| `client.batchMove(ids, targetDirId)` | 批量移动 |
| `client.batchCopy(ids, targetDirId)` | 批量复制 |
| `client.batchSetTags(ids, tags)` | 批量标记 |
| `client.downloadUrl(fileId)` | 获取下载 URL |
| `client.downloadDirUrl(dirId, format?)` | 获取目录下载 URL |
| `client.batchDownloadUrl(ids, format?)` | 获取批量下载 URL |
| `useFileList(config?)` | React Hook：文件列表 |
| `useFileUpload(config?)` | React Hook：文件上传 |
