# cert-manager 工作原理详解

> 深入理解 cert-manager 的架构、组件和工作流程

**作者**: cunzili
**版本**: v1.0
**更新日期**: 2025-01-15

---

## 📋 目录

- [1. cert-manager 架构](#1-cert-manager-架构)
- [2. 核心组件](#2-核心组件)
- [3. 工作流程详解](#3-工作流程详解)
- [4. ACME 协议深度解析](#4-acme-协议深度解析)
- [5. HTTP-01 验证原理](#5-http-01-验证原理)
- [6. DNS-01 验证原理](#6-dns-01-验证原理)
- [7. 证书自动续期机制](#7-证书自动续期机制)

---

## 1. cert-manager 架构

### 1.1 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes 集群                           │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              cert-manager 组件                        │  │
│  │                                                       │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌────────────┐  │  │
│  │  │  Controller  │  │   Webhook    │  │ cainjector │  │  │
│  │  │              │  │              │  │            │  │  │
│  │  │ - 监听资源   │  │ - 验证配置   │  │ - 注入 CA  │  │  │
│  │  │ - 协调流程   │  │ - 准入控制   │  │ - 监听 CRD │  │  │
│  │  └──────────────┘  └──────────────┘  └────────────┘  │  │
│  └───────────────────────────────────────────────────────┘  │
│                          ↕                                    │
│  ┌───────────────────────────────────────────────────────┐ │
│  │              cert-manager CRD（自定义资源）            │ │
│  │                                                        │ │
│  │  - ClusterIssuer                                      │ │
│  │  - Issuer                                             │ │
│  │  - Certificate                                        │ │
│  │  - CertificateRequest                                 │ │
│  │  - Order                                              │ │
│  │  - Challenge                                          │ │
│  └───────────────────────────────────────────────────────┘ │
│                          ↕                                    │
│  ┌───────────────────────────────────────────────────────┐ │
│  │              Kubernetes 资源                           │ │
│  │                                                        │ │
│  │  - Secret（证书存储）                                  │ │
│  │  - Ingress（HTTP 路由）                                │ │
│  │  - Pod（临时验证 Pod）                                 │ │
│  └───────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                          ↕
        ┌─────────────────────────────────────────┐
        │         ACME 服务器（Let's Encrypt）     │
        │                                         │
        │  - 注册账户                              │
        │  - 接收 CSR                              │
        │  - 返回挑战                              │
        │  - 验证挑战                              │
        │  - 签发证书                              │
        └─────────────────────────────────────────┘
```

### 1.2 控制器模型

cert-manager 使用 **Kubernetes 控制器模式**：

```
控制器循环:
┌─────────────────────────────────────────────────────────┐
│                                                         │
│   1. 监听资源变化（Informer）                            │
│      ├─ Certificate 创建/更新/删除                       │
│      ├─ Ingress 创建/更新（带 cert-manager 注解）         │
│      └─ Secret 变化                                     │
│                                                         │
│   2. 对比期望状态和实际状态                              │
│      ├─ 期望: Certificate 需要证书                       │
│      ├─ 实际: Secret 中没有有效证书                      │
│      └─ 差异: 需要申请新证书                             │
│                                                         │
│   3. 执行协调动作（Reconcile）                           │
│      ├─ 创建 CertificateRequest                         │
│      ├─ 向 ACME 服务器申请证书                           │
│      ├─ 完成挑战（HTTP-01/DNS-01）                       │
│      ├─ 获取证书                                        │
│      └─ 更新 Secret                                     │
│                                                         │
│   4. 返回步骤 1（持续运行）                              │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

---

## 2. 核心组件

### 2.1 Controller（控制器）

**作用**: 核心协调器，负责证书的整个生命周期

**功能**:
```
1. 资源监听
   - 监听 Certificate 资源
   - 监听 Ingress 资源（带 cert-manager 注解）
   - 监听 Secret 资源

2. 状态协调
   - 对比期望状态和实际状态
   - 执行协调动作
   - 更新资源状态

3. ACME 客户端
   - 与 ACME 服务器通信
   - 注册账户
   - 提交 CSR
   - 完成挑战
   - 下载证书

4. 证书管理
   - 申请新证书
   - 续期证书
   - 更新 Secret
```

**工作流程**:
```
Controller Loop:
┌──────────────┐
│  监听资源     │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  对比状态     │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  执行协调     │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  更新资源     │
└──────────────┘
       │
       └─→ 循环
```

---

### 2.2 Webhook（验证和准入）

**作用**: 验证配置的正确性

**功能**:
```
1. Validating Webhook（验证）
   - 验证 Certificate 配置
   - 验证 Issuer 配置
   - 阻止无效配置

2. Mutating Webhook（修改）
   - 添加默认值
   - 转换 API 版本

3. Conversion Webhook（转换）
   - 转换不同 API 版本
```

**示例**:
```yaml
# ❌ 无效配置（会被 Webhook 拒绝）
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: myapp-tls
spec:
  secretName: myapp-tls-cert
  issuerRef:
    name: letsencrypt-prod
    # ❌ 缺少 dnsNames 字段

# Webhook 错误:
# Error: secretName is required
# Error: dnsNames must be specified
```

---

### 2.3 cainjector（CA 注入器）

**作用**: 监听 CA 资源变化，注入到 Webhook 和 API Server

**功能**:
```
1. 监听 CA 资源
   - ClusterIssuer
   - Issuer
   - Certificate

2. 提取 CA 证书
   - 从 Secret 中读取 CA 证书
   - 验证 CA 证书有效性

3. 注入 CA 证书
   - 更新 ValidatingWebhookConfiguration
   - 更新 MutatingWebhookConfiguration
   - 更新 APIService

4. 触发重启
   - 当 CA 证书变化时
   - 重启相关 Pod
```

**示例**:
```
场景: 让's Encrypt 中间证书更新

1. Let's Encrypt 更新中间证书
   ↓
2. cert-manager 续期证书
   ↓
3. Secret 中的 CA 证书变化
   ↓
4. cainjector 监听到变化
   ↓
5. cainjector 更新 ValidatingWebhookConfiguration
   ↓
6. Kubernetes API Server 使用新的 CA 证书
```

---

## 3. 工作流程详解

### 3.1 完整证书签发流程

```
用户创建 Certificate
    │
    ▼
┌─────────────────────────────────────────┐
│ 1. cert-manager Controller 监听资源      │
│    - Informer 收到事件                   │
│    - 加入工作队列                       │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 2. 验证配置（Webhook）                   │
│    - 验证 Certificate 配置正确性          │
│    - 验证 Issuer 存在                    │
│    - 验证 dnsNames 不为空                │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 3. 创建 CertificateRequest              │
│    - 生成私钥（RSA 2048）                 │
│    - 生成 CSR（证书签名请求）             │
│    - 创建 CertificateRequest 资源        │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 4. 注册 ACME 账户                       │
│    - 生成账户私钥                        │
│    - 向 ACME 服务器注册                   │
│    - 保存到 Secret（letsencrypt-prod）   │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 5. 提交 CSR 到 ACME 服务器               │
│    - 创建 Order 资源                     │
│    - 提交 CSR                           │
│    - 等待 ACME 服务器响应                 │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 6. ACME 服务器返回 Challenge             │
│    - HTTP-01: token                     │
│    - DNS-01: token + key                │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 7. cert-manager 完成 Challenge           │
│                                          │
│  [HTTP-01]                              │
│    - 创建临时 Pod（solver）               │
│    - 创建临时 Service                    │
│    - 创建临时 Ingress                    │
│    - 配置路由到验证路径                   │
│                                          │
│  [DNS-01]                               │
│    - 调用 DNS API（阿里云/Cloudflare）   │
│    - 创建 TXT 记录                       │
│    - 等待 DNS 生效（1-5 分钟）            │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 8. ACME 服务器验证 Challenge             │
│                                          │
│  [HTTP-01]                              │
│    - 访问: http://domain/.well-known/... │
│    - 验证返回值是否匹配                   │
│                                          │
│  [DNS-01]                               │
│    - 查询: _acme-challenge.domain TXT   │
│    - 验证记录值是否匹配                   │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 9. 验证成功，ACME 服务器签发证书          │
│    - Order 状态: valid                   │
│    - 下载证书（PEM 格式）                 │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 10. 保存证书到 Secret                    │
│     - 创建/更新 Secret                   │
│     - tls.crt: 证书链                    │
│     - tls.key: 私钥                      │
│     - 添加注解（过期时间、序列号等）        │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 11. 更新 Certificate 状态                │
│     - status.conditions:                │
│       - type: Ready                     │
│       - status: "True"                  │
│     - status.lastRenewalTime            │
│     - status.notAfter: 过期时间          │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│ 12. 清理临时资源                         │
│     - 删除临时 Pod/Service/Ingress       │
│     - 删除 DNS TXT 记录                  │
│     - 保留 CertificateRequest（审计）    │
└─────────────────────────────────────────┘
    │
    ▼
✅ 证书就绪，Ingress 自动使用
```

---

### 3.2 状态机

```
Certificate 状态机:
┌──────────────┐
│   Pending    │ ← 初始状态
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  Issuing     │ ← 正在申请
└──────┬───────┘
       │
       ▼
┌──────────────┐
│   Ready      │ ← 证书就绪
└──────┬───────┘
       │
       │ (过期前 30 天)
       ▼
┌──────────────┐
│  Renewing    │ ← 正在续期
└──────┬───────┘
       │
       ▼
┌──────────────┐
│   Ready      │ ← 续期完成
└──────────────┘

失败状态:
┌──────────────┐
│   Failed     │ ← 签发失败
│              │   (会自动重试)
└──────────────┘
```

---

## 4. ACME 协议深度解析

### 4.1 ACME 协议概述

**ACME** (Automatic Certificate Management Environment) 是 IETF 标准（RFC 8555）

**目标**: 自动化证书管理流程

**关键特性**:
- ✅ 无需人工干预
- ✅ 标准化协议
- ✅ 安全（支持多种挑战类型）
- ✅ 可扩展

---

### 4.2 ACME 实体

```
1. 客户端（Client）
   - cert-manager
   - 功能: 申请证书、完成挑战

2. 服务器（Server）
   - Let's Encrypt
   - 功能: 验证域名、签发证书

3. 账户（Account）
   - 客户端在服务器的标识
   - 包含公钥、联系方式

4. 订单（Order）
   - 证书签发请求
   - 包含域名、标识符

5. 授权（Authorization）
   - 域名所有权验证
   - 包含挑战

6. 挑战（Challenge）
   - 验证方式（HTTP-01/DNS-01）
   - 包含 token、key

7. 证书（Certificate）
   - 签发的最终证书
```

---

### 4.3 ACME 协议流程

```
┌──────────────────┐                ┌──────────────────┐
│  cert-manager    │                │  Let's Encrypt   │
│   (客户端)       │                │    (服务器)      │
└────────┬─────────┘                └────────┬─────────┘
         │                                   │
         │  1. 获取目录（Directory）          │
         ├─────────────────────────────────>│
         │                                   │
         │  2. 返回可用 URL                  │
         │<─────────────────────────────────┤
         │  - newNonce (随机数)              │
         │  - newAccount (账户)              │
         │  - newOrder (订单)                │
         │  - revokeCert (吊销)              │
         │                                   │
         │  3. 生成账户密钥对                │
         │  - RSA 2048 / ECDSA P-256         │
         │                                   │
         │  4. 创建账户（newAccount）         │
         ├─────────────────────────────────>│
         │  - JWK 公钥                       │
         │  - 联系方式（email）              │
         │                                   │
         │  5. 返回账户 URL                  │
         │<─────────────────────────────────┤
         │  - https://acme-v02.api.../acme/│
         │    acct/123456789                 │
         │                                   │
         │  6. 创建订单（newOrder）           │
         ├─────────────────────────────────>│
         │  - identifiers:                   │
         │    - type: dns                    │
         │    - value: www.example.com       │
         │                                   │
         │  7. 返回订单 URL                  │
         │<─────────────────────────────────┤
         │  - https://acme-v02.api.../order/│
         │    987654321                      │
         │  - authorizations: []             │
         │  - finalize: URL                  │
         │                                   │
         │  8. 获取授权（Authorization）      │
         ├─────────────────────────────────>│
         │                                   │
         │  9. 返回挑战（Challenge）          │
         │<─────────────────────────────────┤
         │  - type: http-01                  │
         │  - status: pending                │
         │  - token: 随机字符串               │
         │  - url: 挑战 URL                  │
         │                                   │
         │  10. 完成挑战（HTTP-01/DNS-01）     │
         ├─────────────────────────────────>│
         │  - keyAuthorization: token.key    │
         │                                   │
         │  11. ACME 服务器验证挑战           │
         │  - HTTP-01: 访问验证 URL           │
         │  - DNS-01: 查询 DNS TXT 记录      │
         │                                   │
         │  12. 挑战成功                     │
         │<─────────────────────────────────┤
         │  - status: valid                  │
         │                                   │
         │  13. 提交 CSR（finalize）          │
         ├─────────────────────────────────>│
         │  - csr: DER 编码的 CSR             │
         │                                   │
         │  14. ACME 服务器签发证书           │
         │<─────────────────────────────────┤
         │  - certificate: 证书 URL           │
         │                                   │
         │  15. 下载证书                     │
         ├─────────────────────────────────>│
         │                                   │
         │  16. 返回证书（PEM 格式）           │
         │<─────────────────────────────────┤
         │  - 证书链                          │
         │  - 中间证书                        │
         │                                   │
         ▼                                   ▼
    证书就绪
```

---

## 5. HTTP-01 验证原理

### 5.1 验证流程

```
目标: 证明 cert-manager 控制 domain.com

1. ACME 服务器生成 Challenge
   - Token: "LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0"
   - 要求: 在 http://domain.com/.well-known/acme-challenge/LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0
           放置内容: "LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0.<key>"

2. cert-manager 完成挑战
   - Key Authorization: Base64URL(Thumbprint(JWK) + "." + Token)
   - 例如: "LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0.abc123..."

3. cert-manager 创建临时资源
   - Pod (solver): 返回 Key Authorization
   - Service: 暴露 Pod
   - Ingress: 路由 /.well-known/acme-challenge/* → Pod

4. ACME 服务器验证
   - 访问: http://domain.com/.well-known/acme-challenge/LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0
   - 验证: 返回值是否匹配 "LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0.<key>"

5. 验证成功
   - Challenge 状态: pending → valid
   - 继续证书签发流程
```

---

### 5.2 cert-manager 创建的临时资源

```yaml
# 1. 临时 Pod (cm-acme-http-solver-xxx)
apiVersion: v1
kind: Pod
metadata:
  name: cm-acme-http-solver-xxx
  labels:
    acme.cert-manager.io/http-domain: "12345"
spec:
  containers:
  - name: acmesolver
    image: quay.io/jetstack/cert-manager-acmesolver:v1.13.0
    args:
    - -- HTTP-01
    - --domain=domain.com
    - --token=LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0
    - --key=abc123...
  restartPolicy: OnFailure

---
# 2. 临时 Service
apiVersion: v1
kind: Service
metadata:
  name: cm-acme-http-solver-xxx
  labels:
    acme.cert-manager.io/http-domain: "12345"
spec:
  ports:
  - port: 8089
    targetPort: 8089
  selector:
    acme.cert-manager.io/http-domain: "12345"

---
# 3. 临时 Ingress
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: cm-acme-http-solver-xxx
  labels:
    acme.cert-manager.io/http-domain: "12345"
spec:
  ingressClassName: nginx
  rules:
  - host: domain.com
    http:
      paths:
      - path: /.well-known/acme-challenge/LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0
        pathType: Exact
        backend:
          service:
            name: cm-acme-http-solver-xxx
            port:
              number: 8089
```

---

### 5.3 HTTP-01 的要求

```
必需条件:
✅ 域名已解析到服务器 IP
✅ 80 端口可从公网访问
✅ Ingress Controller 已运行
✅ 临时 Ingress 可正常工作

不支持:
❌ 非 HTTP 服务（如 TCP、UDP）
❌ 通配符证书（*.domain.com）
❌ 端口非 80（如 8080）

常见问题:
1. 域名未解析: 修改 DNS 后等待生效
2. 防火墙阻止: 开放 80 端口
3. Ingress Controller 未运行: 检查 Pod 状态
4. 临时 Ingress 失败: 查看 cert-manager 日志
```

---

## 6. DNS-01 验证原理

### 6.1 验证流程

```
目标: 证明 cert-manager 控制 domain.com 的 DNS

1. ACME 服务器生成 Challenge
   - Token: "LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0"
   - 要求: 在 _acme-challenge.domain.com 配置 TXT 记录
           内容: "<key>"

2. cert-manager 完成挑战
   - Key Authorization: Base64URL(Thumbprint(JWK) + "." + Token)
   - 例如: "abc123xyz456..."

3. cert-manager 调用 DNS API
   - Cloudflare API
   - 阿里云 DNS API
   - 腾讯云 DNS API
   - 创建 TXT 记录: _acme-challenge.domain.com → "abc123xyz456..."
   - 等待 DNS 生效（1-5 分钟）

4. ACME 服务器验证
   - 查询: _acme-challenge.domain.com TXT
   - 验证: 记录值是否匹配

5. 验证成功
   - Challenge 状态: pending → valid
   - 继续证书签发流程
```

---

### 6.2 DNS API 配置示例

#### Cloudflare

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - dns01:
        cloudflare:
          email: cloudflare@example.com
          apiTokenSecretRef:
            name: cloudflare-api-token
            key: api-token
```

```bash
# 创建 Secret
kubectl create secret generic cloudflare-api-token \
  --from-literal=api-token=YOUR_CLOUDFLARE_API_TOKEN
```

---

#### 阿里云 DNS

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - dns01:
        alidns:
          accessKeySecretRef:
            name: alidns-secret
            key: access-key
          secretKeySecretRef:
            name: alidns-secret
            key: secret-key
```

```bash
# 创建 Secret
kubectl create secret generic alidns-secret \
  --from-literal=access-key=YOUR_ACCESS_KEY \
  --from-literal=secret-key=YOUR_SECRET_KEY
```

---

### 6.3 DNS-01 的优势

```
✅ 支持通配符证书
   - *.domain.com
   - 多个子域名共用一个证书

✅ 不需要 80 端口
   - 可以申请任何服务的证书
   - 可以在内部网络使用

✅ 验证更可靠
   - 不受网络波动影响
   - 不受 Ingress 配置影响

✅ 支持多域名证书
   - 一次验证多个域名
   - 节省时间和资源
```

---

## 7. 证书自动续期机制

### 7.1 续期触发条件

```
cert-manager 在以下情况下续期证书:

1. 过期前 30 天（默认）
   - Certificate.spec.renewBefore: 720h (30天)
   - 计算公式: notAfter - renewBefore = 续期时间

2. Secret 中的证书过期
   - 检查 Secret 的 tls.crt
   - 解析证书的 notAfter 字段
   - 判断是否需要续期

3. Certificate 资源变更
   - 修改 dnsNames
   - 修改 issuerRef
   - 修改 duration/renewBefore
```

---

### 7.2 续期流程

```
1. cert-manager Controller 定期检查证书
   - 默认: 每 1 小时
   - 可配置: --certificate-renew-percentage=66.67

2. 检测到需要续期
   - 当前时间 >= notAfter - renewBefore
   - 例如: 2025-02-14 >= 2025-03-15 - 30天

3. 创建新的 CertificateRequest
   - 生成新私钥
   - 生成新 CSR
   - 创建 CertificateRequest 资源

4. 申请新证书
   - 重复证书签发流程
   - 向 ACME 服务器申请

5. 获取新证书后
   - 更新 Secret（直接覆盖）
   - tls.crt: 新证书
   - tls.key: 新私钥

6. Ingress 自动使用新证书
   - Ingress Controller 重新加载配置
   - 无需重启 Pod

7. 保留旧 CertificateRequest
   - 用于审计
   - 保留时间: 默认永久
```

---

### 7.3 续期策略

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: myapp-tls
spec:
  secretName: myapp-tls-cert
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - www.example.com

  # 证书有效期（默认 90 天）
  duration: 2160h  # 90天

  # 续期时间（过期前 30 天）
  renewBefore: 720h  # 30天

  # 重试次数（默认 -1，无限重试）
  revisionHistoryLimit: 3
```

---

### 7.4 续期失败处理

```
如果续期失败:

1. cert-manager 自动重试
   - 重试间隔: 指数退避（1分钟、5分钟、15分钟...）
   - 最大重试次数: 无限制（默认）

2. 更新 Certificate 状态
   - status.conditions:
     - type: Renewing
     - status: "False"
     - message: "Failed to wait for certificate order..."

3. 发送事件（Event）
   - kubectl describe certificate myapp-tls
   - Events:
     - Failed to wait for certificate order...

4. 监控告警
   - Prometheus 指标: certificate_ready_status
   - 设置告警: 证书即将过期

5. 手动介入
   - 查看日志: kubectl logs -n cert-manager deployment/cert-manager
   - 删除 CertificateRequest（强制重试）
   - 修改 Certificate 配置
```

---

## 总结

### 关键要点

1. **架构**: Controller、Webhook、cainjector 三个组件协同工作
2. **工作流程**: 监听 → 验证 → 申请 → 挑战 → 签发 → 保存
3. **ACME 协议**: 标准化的证书自动化管理协议
4. **HTTP-01**: 通过 HTTP 访问验证域名所有权（简单）
5. **DNS-01**: 通过 DNS 记录验证域名所有权（支持通配符）
6. **自动续期**: 过期前 30 天自动续期

### 学习建议

```
理解原理 → 阅读本部分（02-how-it-works.md）
    ↓
安装部署 → 跟随安装指南（03-installation.md）
    ↓
配置实战 → 查看配置指南（04-configuration.md）
    ↓
解决问题 → 参考故障排查（05-troubleshooting.md）
```

---

**下一步**: [03-installation.md](./03-installation.md) 🚀
