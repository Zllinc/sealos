# cert-manager 完整指南

> Kubernetes 证书自动化管理工具 - 从原理到实战

**作者**: cunzili
**版本**: v1.0
**更新日期**: 2025-01-15

---

## 📚 文档结构

本文档集全面讲解 cert-manager 的原理、安装、配置和实战。

### 核心文档（推荐阅读顺序）

```
01-what-is-certmanager.md  ← 从这里开始
    ├─ 什么是 cert-manager
    ├─ 为什么需要它
    ├─ 核心概念
    └─ 应用场景
         ↓
02-how-it-works.md         ← 深入理解
    ├─ 工作原理
    ├─ 架构设计
    ├─ ACME 协议
    └─ 验证方式
         ↓
03-installation.md         ← 安装部署
    ├─ 前置条件
    ├─ 安装步骤
    ├─ 验证安装
    └─ 卸载方法
         ↓
04-configuration.md        ← 配置实战
    ├─ ClusterIssuer 配置
    ├─ Certificate 配置
    ├─ Ingress 集成
    ├─ DNS 验证配置
    └─ 完整示例
         ↓
05-troubleshooting.md      ← 问题解决
    ├─ 常见问题
    ├─ 诊断方法
    ├─ 日志查看
    └─ 解决方案
```

---

## 🎯 学习目标

### 阅读完本文档后，你将能够：

✅ 理解 cert-manager 的核心概念和工作原理
✅ 独立安装和配置 cert-manager
✅ 自动签发和续期 Let's Encrypt 证书
✅ 配置 HTTP-01 和 DNS-01 验证
✅ 排查证书签发失败的问题
✅ 在生产环境中使用 cert-manager

---

## 📖 内容概览

### 01-what-is-certmanager.md - cert-manager 介绍（8KB）

**适合**: 想了解 cert-manager 是什么的读者

**内容**:
- 什么是 cert-manager
- 为什么需要证书自动化管理
- 核心概念（Issuer、ClusterIssuer、Certificate）
- ACME 协议简介
- 应用场景

**预计时间**: 15 分钟

---

### 02-how-it-works.md - 工作原理（10KB）

**适合**: 想深入理解原理的读者

**内容**:
- cert-manager 架构
- 工作流程详解
- ACME 协议工作原理
- HTTP-01 验证原理
- DNS-01 验证原理
- 证书自动续期机制

**亮点**:
- 📊 架构图和流程图
- 🔍 详细的原理解释
- 💡 两种验证方式的对比

**预计时间**: 20 分钟

---

### 03-installation.md - 安装指南（6KB）

**适合**: 准备安装 cert-manager 的读者

**内容**:
- 前置条件检查
- 使用 YAML 安装
- 使用 Helm 安装
- 验证安装
- 卸载步骤

**亮点**:
- ⚡ 快速安装命令
- 🔍 验证方法
- ⚠️ 注意事项

**预计时间**: 10 分钟 + 安装时间

---

### 04-configuration.md - 配置实战（12KB）

**适合**: 准备配置证书的读者

**内容**:
- ClusterIssuer 配置详解
- Certificate 资源配置
- Ingress 集成配置
- HTTP-01 验证配置
- DNS-01 验证配置（阿里云/Cloudflare）
- 完整的生产级示例
- 证书续期验证

**亮点**:
- 🏢 生产级配置示例
- 🔧 多种验证方式
- 📝 详细的注释说明

**预计时间**: 30 分钟 + 实践时间

---

### 05-troubleshooting.md - 故障排查（8KB）

**适合**: 遇到问题的读者

**内容**:
- 5 大常见问题
- 诊断流程
- 日志查看技巧
- 问题定位方法
- 解决方案

**问题列表**:
1. 证书签发失败（Ready=False）
2. HTTP-01 验证失败
3. DNS-01 验证失败
4. 证书无法自动续期
5. Ingress 无法访问

**亮点**:
- 🔍 系统化诊断流程
- 💊 问题-原因-解决格式
- 📋 快速检查清单

**预计时间**: 根据问题而定

---

## 🚀 快速开始

### 第一次使用

```
1. 阅读 01-what-is-certmanager.md（了解概念）
2. 跟随 03-installation.md（安装 cert-manager）
3. 跟随 04-configuration.md（配置证书）
4. 遇到问题查看 05-troubleshooting.md（解决）
```

### 快速查阅

```
需要理解原理 → 02-how-it-works.md
需要配置证书 → 04-configuration.md
遇到问题 → 05-troubleshooting.md
```

---

## 📊 核心概念预览

### cert-manager 是什么？

**cert-manager** 是 Kubernetes 的证书自动化管理工具，可以：
- ✅ 自动签发 TLS/SSL 证书
- ✅ 自动续期证书（过期前 30 天）
- ✅ 支持多种证书来源（Let's Encrypt、自签名、内部 CA）
- ✅ 与 Kubernetes Ingress 无缝集成

### 核心资源

| 资源 | 作用 | 范围 |
|------|------|------|
| **Issuer** | 证书签发者 | 单个 Namespace |
| **ClusterIssuer** | 集群级证书签发者 | 整个集群 |
| **Certificate** | 证书资源 | 自动管理 Secret |
| **CertificateRequest** | 证书签发请求 | 短期资源 |

### 工作流程

```
用户创建 Certificate
    ↓
cert-manager 检测到新资源
    ↓
向 ACME 服务器（Let's Encrypt）申请证书
    ↓
ACME 服务器返回挑战（HTTP-01 或 DNS-01）
    ↓
cert-manager 完成挑战（验证域名所有权）
    ↓
ACME 服务器签发证书
    ↓
cert-manager 将证书保存到 Secret
    ↓
证书过期前 30 天自动续期
```

---

## 💡 为什么需要 cert-manager？

### 传统方式的问题

```
1. 手动申请证书
   - 登录证书服务商
   - 填写 CSR
   - 验证域名
   - 下载证书
   - 手动上传到 Kubernetes
   - 手动更新 Ingress

2. 手动续期
   - 证书有效期 90 天
   - 需要记住续期时间
   - 重复上述流程

3. 容易出错
   - 忘记续期 → 证书过期 → 网站无法访问
   - 配置错误 → 证书无效
   - Secret 不同步
```

### cert-manager 的优势

```
✅ 完全自动化
   - 自动签发
   - 自动续期
   - 自动部署到 Kubernetes

✅ 声明式配置
   - 用 YAML 定义证书需求
   - GitOps 友好
   - 版本控制

✅ 无缝集成
   - 与 Ingress 集成
   - 自动更新 Secret
   - 支持多种 Ingress Controller

✅ 安全可靠
   - 私钥安全存储
   - 自动续期（过期前 30 天）
   - 支持多种验证方式
```

---

## 🎯 适用场景

### 场景 1: 个人网站/博客

```
需求:
- 域名: www.example.com
- 证书: Let's Encrypt（免费）
- 验证方式: HTTP-01

解决方案:
- cert-manager + Let's Encrypt
- 自动签发和续期
- 零成本 HTTPS
```

### 场景 2: 生产环境应用

```
需求:
- 多个域名: *.example.com, api.example.com
- 证书: Let's Encrypt 或商业证书
- 验证方式: DNS-01（支持通配符）

解决方案:
- cert-manager + Let's Encrypt + DNS 验证
- 自动管理多个域名
- 支持通配符证书
```

### 场景 3: 企业内部应用

```
需求:
- 内部域名: app.internal
- 证书: 内部 CA 或自签名
- 不需要公网验证

解决方案:
- cert-manager + 自签名 CA
- 内部证书自动化
- 无需外部依赖
```

---

## 📋 前置知识

在开始之前，建议了解：

1. **Kubernetes 基础**
   - Pod、Deployment、Service
   - Ingress 概念
   - Secret 资源

2. **DNS 基础**
   - 域名解析
   - A 记录、CNAME 记录
   - DNS 服务器

3. **TLS/SSL 基础**
   - 证书的作用
   - 公钥/私钥
   - CA（证书颁发机构）

**推荐先阅读**:
- [Kubernetes Ingress 文档](../webhook/01-concepts.md)

---

## 🔄 更新日志

**v1.0** (2025-01-15)
- 初始版本
- 完整的 cert-manager 指南
- 包含原理、安装、配置、故障排查

---

## 📚 参考资料

- [cert-manager 官方文档](https://cert-manager.io/docs/)
- [cert-manager GitHub](https://github.com/cert-manager/cert-manager)
- [Let's Encrypt](https://letsencrypt.org/)
- [ACME 协议规范](https://datatracker.ietf.org/doc/html/rfc8555)
- [Kubernetes Ingress 文档](../webhook/00-INDEX.md)

---

**开始学习**: [01-what-is-certmanager.md](./01-what-is-certmanager.md) 🚀
