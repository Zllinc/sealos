# Commit 时为什么需要下载镜像？

## 问题描述

在执行 Devbox commit 操作时，虽然容器已经创建成功，但在 commit 阶段仍然出现镜像下载日志：

```
container created successfully: 52e07683b5e9eb5f27f0ce0f77729ef4936aa36d880fa0ff141bb7c1c71ee792
ghcr.io/labring-actions/devbox/go-1.23.0:13aacd8: resolving
index-sha256:d20f38e2b951197c7d0b5fff56720f46113f91035ea0f9d7482e6b8fb41ec0f1: exists
manifest-sha256:...: exists
config-sha256:...: exists
layer-sha256:...: waiting → done
```

**疑问**：镜像下载不应该在创建容器时完成吗？为什么 commit 时还要下载？

## 根本原因

### 1. 容器创建 vs Commit 的不同需求

#### 容器创建时的镜像需求

```go
// CreateContainer 中的配置
createOpt := types.ContainerCreateOptions{
    Pull: "missing",  // 如果镜像不存在才拉取
    // ...
}
```

**容器创建只需要**：
- 镜像的 manifest（知道有哪些 layer）
- 镜像的 config（容器配置）
- **已存在的 layer content**（用于创建 snapshot）

**关键点**：如果镜像的 manifest 和 config 存在，即使某些 layer 的 content 缺失，容器创建也可能成功（因为使用的是 snapshot，不是直接读取 layer）。

#### Commit 时的镜像需求

**Commit 操作需要**：
- 完整的镜像 manifest
- 完整的镜像 config（用于生成新镜像的 config）
- **所有 layer 的完整 content**（用于验证和生成新的 manifest）

### 2. Content Store 的引用计数机制

```
┌─────────────────────────────────────────────────────────────┐
│                  Containerd Content Store                    │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │              Image Metadata (存在)                  │    │
│  │  - index-sha256: d20f38e2... (exists)              │    │
│  │  - manifest-sha256: b51ebbe0... (exists)           │    │
│  │  - config-sha256: e6eeacda... (exists)             │    │
│  └────────────────────────────────────────────────────┘    │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │              Layer Content (部分缺失)                │    │
│  │  - layer-sha256:5505aeed... (waiting → 需要下载)    │    │
│  │  - layer-sha256:862ff86a... (waiting → 需要下载)    │    │
│  │  - layer-sha256:fb893852... (waiting → 需要下载)    │    │
│  │  - ... (其他 layer 同样缺失)                         │    │
│  └────────────────────────────────────────────────────┘    │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

**为什么会出现这种情况？**

1. **镜像元数据保留**：Image reference 和 manifest/config 通常保留在 containerd 中
2. **Layer content 可能被 GC**：当磁盘空间不足时，Kubelet 或 containerd GC 可能删除 layer 的 content blob
3. **引用计数**：只要 manifest/config 存在，镜像 reference 就存在，但 layer content 可能因为引用计数为 0 被删除

### 3. Commit 操作的完整流程

```
┌─────────────────────────────────────────────────────────────┐
│                    Commit 操作流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│   1. CreateContainer()                                      │
│      └── Pull: "missing"                                    │
│      └── 检查镜像是否存在（检查 manifest/config）            │
│      └── 如果 manifest 存在，容器创建成功                    │
│      └── ⚠️ 不验证 layer content 是否完整                   │
│                                                              │
│   2. container.Commit()                                     │
│      └── 读取基础镜像的完整信息                              │
│      └── 需要生成新镜像的 manifest                           │
│      └── 需要生成新镜像的 config                             │
│      └── 需要验证所有 layer 的完整性                         │
│                                                              │
│   3. 发现 layer content 缺失                                 │
│      └── 触发自动拉取缺失的 layer                            │
│      └── 显示下载日志                                        │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### 4. 为什么容器创建时没有发现 layer 缺失？

**容器创建使用的是 snapshot**：

```
Container Creation:
  ┌─────────────┐
  │ Base Image  │ ──┐
  └─────────────┘   │
                     ├──> Snapshot (已存在，基于之前的 layer)
  ┌─────────────┐   │
  │   Container │ ──┘
  └─────────────┘
```

- 容器创建时，如果 snapshot 已经存在（基于之前的 layer），就不需要重新读取 layer content
- Snapshot 是文件系统的快照，独立于原始 layer content

**Commit 需要原始 layer**：

```
Commit Operation:
  ┌─────────────┐
  │ Base Image  │ ──┐
  └─────────────┘   │
                     ├──> 需要读取完整的 layer content
  ┌─────────────┐   │     (用于生成新的 manifest)
  │ Commit Image│ ──┘
  └─────────────┘
```

- Commit 需要读取基础镜像的完整 layer 信息来生成新镜像的 manifest
- 如果 layer content 缺失，必须重新下载

## 日志分析

从你提供的日志可以看出：

```
1. container created successfully: 52e07683b5e9eb5f27f0ce0f77729ef4936aa36d880fa0ff141bb7c1c71ee792
   └── 容器创建成功（说明 manifest/config 存在）

2. ghcr.io/.../go-1.23.0:13aacd8: resolving
   └── Commit 开始解析镜像

3. index-sha256:d20f38e2...: exists
   manifest-sha256:b51ebbe0...: exists
   config-sha256:e6eeacda...: exists
   └── 元数据都存在

4. layer-sha256:5505aeed...: waiting → done
   layer-sha256:862ff86a...: waiting → done
   ... (其他 layer)
   └── Layer content 缺失，需要下载
```

## 触发条件

以下情况会导致 commit 时下载镜像：

1. **磁盘空间不足触发 GC**：
   - Kubelet Image GC 删除了 layer content
   - Containerd Content GC 删除了未引用的 blob

2. **手动删除镜像**：
   - 使用 `ctr images rm` 删除镜像时，可能只删除了 layer content，保留了 manifest/config

3. **跨 namespace 问题**：
   - 如果镜像在 `k8s.io` namespace 中被删除，但 `sealos.io` namespace 中还有引用
   - 可能导致 layer content 被 GC，但 manifest/config 保留

4. **部分拉取**：
   - 如果之前只拉取了镜像的 manifest/config，没有拉取所有 layer

## 解决方案

### 方案 1：确保镜像完整拉取（推荐）

在创建容器时强制拉取完整镜像：

```go
createOpt := types.ContainerCreateOptions{
    Pull: "always",  // 改为 always，确保完整拉取
    // ...
}
```

**缺点**：每次创建容器都会重新拉取，耗时较长

### 方案 2：Commit 前验证镜像完整性

在 commit 前检查镜像是否完整，如不完整则重新拉取：

```go
func (c *CommitterImpl) ensureImageComplete(ctx context.Context, baseImage string) error {
    img, err := c.containerdClient.GetImage(ctx, baseImage)
    if err != nil {
        return c.pullImage(ctx, baseImage)
    }
    
    // 检查所有 layer 是否存在
    config, err := img.Config(ctx)
    if err != nil {
        // config 缺失，需要重新拉取
        return c.pullImage(ctx, baseImage)
    }
    
    manifest, err := img.Manifest(ctx)
    if err != nil {
        return c.pullImage(ctx, baseImage)
    }
    
    // 验证所有 layer 的 content 是否存在
    for _, layer := range manifest.Layers {
        _, err := c.containerdClient.ContentStore().Info(ctx, layer.Digest)
        if err != nil {
            // layer 缺失，需要重新拉取
            return c.pullImage(ctx, baseImage)
        }
    }
    
    return nil
}
```

### 方案 3：使用 `Pull: "missing"` 但接受 commit 时的自动拉取

保持当前实现，允许 commit 时自动拉取缺失的 layer。这是 nerdctl 的默认行为，会自动处理。

**优点**：
- 不需要修改代码
- nerdctl 会自动处理缺失的 layer

**缺点**：
- commit 时间可能变长
- 可能因为网络问题导致 commit 失败

## 总结

**为什么 commit 时需要下载镜像？**

1. **容器创建**只需要镜像的 manifest/config 和已存在的 snapshot，不验证 layer content 完整性
2. **Commit 操作**需要完整的镜像信息（包括所有 layer content）来生成新镜像的 manifest
3. **Layer content 可能被 GC 删除**，但 manifest/config 保留，导致 commit 时发现缺失并自动拉取

这是 **正常行为**，nerdctl 会自动处理缺失的 layer。如果希望避免，可以在创建容器时使用 `Pull: "always"` 或实现镜像完整性检查。

## 相关代码

| 文件 | 说明 |
|------|------|
| `internal/commit/commit.go` | Commit 实现，调用 `container.Commit()` |
| `internal/commit/commit.go:141` | `Pull: "missing"` 配置 |

## 参考资料

- [Containerd Content Store](https://github.com/containerd/containerd/blob/main/docs/content-flow.md)
- [Nerdctl Commit Implementation](https://github.com/containerd/nerdctl)

## 更新日志

| 日期 | 版本 | 更新内容 |
|------|------|----------|
| 2025-12-12 | v1.0 | 初始版本 |

