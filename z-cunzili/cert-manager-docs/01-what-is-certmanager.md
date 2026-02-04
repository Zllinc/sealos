# cert-manager 是什么？

> Kubernetes 证书自动化管理工具全面解析

**作者**: cunzili
**版本**: v1.0
**更新日期**: 2025-01-15

---

## 📋 目录

- [1. 什么是 cert-manager](#1-什么是-cert-manager)
- [2. 为什么需要 cert-manager](#2-为什么需要-cert-manager)
- [3. 核心概念](#3-核心概念)
- [4. ACME 协议简介](#4-acme-协议简介)
- [5. 证书来源](#5-证书来源)
- [6. 应用场景](#6-应用场景)
- [7. 与 Kubernetes 的集成](#7-与-kubernetes-的集成)

---

## 1. 什么是 cert-manager

### 1.1 定义

**cert-manager** 是一个 Kubernetes 原生的证书管理工具，用于：

- ✅ 自动签发 TLS/SSL 证书
- ✅ 自动续期证书（过期前 30 天）
- ✅ 自动部署证书到 Kubernetes 集群
- ✅ 支持多种证书来源（Let's Encrypt、自签名、内部 CA）
- ✅ 与 Kubernetes Ingress 无缝集成

### 1.2 核心价值

```
传统方式:
手动申请证书 → 手动配置 → 手动续期 → 容易过期 ❌

cert-manager:
声明式配置 → 自动申请 → 自动续期 → 永不过期 ✅
```

### 1.3 工作概述

```
你只需要:
1. 用 YAML 定义证书需求
2. 创建 ClusterIssuer（证书签发者）
3. 在 Ingress 中添加注解

cert-manager 自动:
1. 监听证书需求
2. 向 ACME 服务器（Let's Encrypt）申请证书
3. 完成域名验证（HTTP-01 或 DNS-01）
4. 获取证书并保存到 Kubernetes Secret
5. 过期前 30 天自动续期
6. 更新 Secret（Ingress 自动生效）
```

### 1.4 架构定位

```
Kubernetes 集群:
├── cert-manager
│   ├── Controller（控制器）
│   ├── Webhook（验证和准入）
│   ├── CA Injector（CA 注入）
│   └── cainjector（CA 注入器）
│
├── 你的应用
│   ├── Deployment
│   ├── Service
│   └── Ingress ← cert-manager 自动注入证书
│
└── Kubernetes 资源
    ├── Issuer/ClusterIssuer（证书签发者）
    ├── Certificate（证书需求）
    └── Secret（证书存储）
```

---

## 2. 为什么需要 cert-manager

### 2.1 传统证书管理的痛点

#### 痛点 1: 手动操作繁琐

```
传统流程:
1. 登录证书服务商（Let's Encrypt/阿里云/DigiCert）
2. 填写 CSR（证书签名请求）
3. 验证域名所有权
   - 上传文件到 Web 服务器
   - 或配置 DNS TXT 记录
4. 等待验证
5. 下载证书文件（.crt 和 .key）
6. 手动创建 Kubernetes Secret
7. 手动更新 Ingress 配置

问题:
- 耗时: 每次约 30-60 分钟
- 易错: 多个步骤，容易出错
- 重复: 每个证书都需要重复
```

#### 痛点 2: 证书续期麻烦

```
Let's Encrypt 证书:
- 有效期: 90 天
- 需要在过期前续期

传统续期流程:
1. 记住续期时间（或设置日历提醒）
2. 重复上述申请流程
3. 更新 Kubernetes Secret
4. 更新 Ingress 配置
5. 重启负载

问题:
- 忘记续期 → 证书过期 → 网站无法访问
- 续期不及时 → 用户体验差
- 多个证书 → 管理复杂
```

#### 痛点 3: 配置容易出错

```
常见错误:
1. Secret 配置错误
   - 证书和私钥不匹配
   - 证书格式错误
   - Secret 名称错误

2. Ingress 配置错误
   - Secret 名称不一致
   - TLS 配置错误
   - 证书链不完整

3. 证书过期
   - 忘记续期
   - 续期失败
   - 监控缺失

后果:
- 浏览器警告: "您的连接不是私密连接"
- 用户无法访问
- SEO 降权
- 业务损失
```

### 2.2 cert-manager 的优势

#### 优势 1: 完全自动化

```
cert-manager 流程:
1. 用 YAML 定义证书需求（一次性）
2. cert-manager 自动申请证书
3. cert-manager 自动完成域名验证
4. cert-manager 自动保存到 Secret
5. cert-manager 自动续期（过期前 30 天）
6. cert-manager 自动更新 Secret

时间成本:
- 首次配置: 5-10 分钟
- 后续维护: 0 分钟（全自动）
```

#### 优势 2: 声明式配置

```yaml
# 只需定义"我想要什么"
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
    - http01:
        ingress:
          class: nginx
---
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
  - example.com

# cert-manager 自动处理"如何实现"
```

#### 优势 3: GitOps 友好

```
传统方式:
- 手动在 Web 控制台操作
- 配置无法版本控制
- 无法审计和回滚

cert-manager:
- 所有配置用 YAML 定义
- 可以用 Git 管理
- 可以 Code Review
- 可以版本控制
- 可以自动化部署（CI/CD）
- 可以快速回滚
```

#### 优势 4: 与 Kubernetes 无缝集成

```yaml
# Ingress 中直接引用
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    # cert-manager 注解（自动签发证书）
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    secretName: myapp-tls-cert  # cert-manager 自动创建
  rules:
  - host: www.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp-service
            port:
              number: 80

# cert-manager 自动:
# 1. 监听 Ingress 创建
# 2. 检测到 cert-manager.io/cluster-issuer 注解
# 3. 自动申请证书
# 4. 自动创建 Secret: myapp-tls-cert
# 5. 自动续期
```

#### 优势 5: 安全可靠

```
安全性:
✅ 私钥安全存储（Kubernetes Secret）
✅ 支持私钥加密（可选）
✅ 支持 RBAC 权限控制
✅ 支持证书轮换
✅ 支持密钥加密存储

可靠性:
✅ 自动续期（过期前 30 天）
✅ 续期失败自动重试
✅ 支持多个 ACME 服务器
✅ 支持备份和恢复
✅ 丰富的监控指标
```

---

## 3. 核心概念

### 3.1 资源类型

#### 1. Issuer（证书签发者）

**作用**: 定义证书签发方式（单 Namespace）

```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: letsencrypt-prod
  namespace: default  # 仅在 default namespace 有效
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - http01:
        ingress:
          class: nginx
```

**特点**:
- 作用域: 单个 Namespace
- 使用场景: 测试环境、单应用

---

#### 2. ClusterIssuer（集群级证书签发者）

**作用**: 定义证书签发方式（整个集群）

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
  # 没有 namespace 字段（集群级别）
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - http01:
        ingress:
          class: nginx
```

**特点**:
- 作用域: 整个集群
- 使用场景: 生产环境、多应用

**对比**:

| 特性 | Issuer | ClusterIssuer |
|------|--------|---------------|
| **作用域** | 单 Namespace | 整个集群 |
| **权限** | 需要创建 Role | 需要创建 ClusterRole |
| **推荐场景** | 测试环境 | 生产环境 |

---

#### 3. Certificate（证书资源）

**作用**: 定义证书需求

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: myapp-tls
  namespace: production
spec:
  # 证书存储的 Secret
  secretName: myapp-tls-cert

  # 引用 Issuer
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer

  # 证书包含的域名
  dnsNames:
  - www.example.com
  - example.com

  # 证书有效期（默认 90 天）
  duration: 2160h  # 90 天
  renewBefore: 720h  # 30 天后续期

  # 证书算法（默认 RSA 2048）
  keyAlgorithm: rsa
  keySize: 2048
```

**特点**:
- 声明式配置
- 自动管理 Secret
- 自动续期

---

#### 4. CertificateRequest（证书签发请求）

**作用**: 短期资源，记录每次证书签发请求

```bash
# 查看 CertificateRequest
kubectl get certificaterequest

# 输出:
# NAME                         AGE   STATE
# myapp-tls-1                  5m    issued
# myapp-tls-2                  60d   issued
```

**特点**:
- 短期资源（签发后自动清理）
- 用于审计和调试
- 记录每次签发详情

---

#### 5. Secret（证书存储）

**作用**: 存储签发的证书

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: myapp-tls-cert
  namespace: production
type: kubernetes.io/tls
data:
  tls.crt: <base64 编码的证书>
  tls.key: <base64 编码的私钥>
```

**特点**:
- 由 cert-manager 自动创建和管理
- 不要手动修改
- Ingress 直接引用

---

### 3.2 工作流程

```
1. 用户创建 ClusterIssuer
   ↓
   定义证书签发方式（Let's Encrypt）
   ↓

2. 用户创建 Certificate
   ↓
   定义证书需求（域名、有效期）
   ↓

3. cert-manager Controller 监听到 Certificate
   ↓
   创建 CertificateRequest
   ↓

4. cert-manager 向 ACME 服务器（Let's Encrypt）注册账户
   ↓
   生成私钥
   ↓
   保存到 Secret（letsencrypt-prod）
   ↓

5. cert-manager 向 ACME 服务器申请证书
   ↓
   提交 CSR（证书签名请求）
   ↓

6. ACME 服务器返回挑战（Challenge）
   ↓
   HTTP-01: 在特定路径放置文件
   ↓ 或
   DNS-01: 配置 DNS TXT 记录
   ↓

7. cert-manager 完成挑战
   ↓
   HTTP-01: 创建临时 Ingress/Pod
   ↓ 或
   DNS-01: 调用 DNS API 配置 TXT 记录
   ↓

8. ACME 服务器验证挑战
   ↓
   HTTP-01: 访问 http://domain/.well-known/acme-challenge/token
   ↓ 或
   DNS-01: 查询 DNS TXT 记录
   ↓

9. 验证成功
   ↓
   ACME 服务器签发证书
   ↓

10. cert-manager 下载证书
    ↓
    保存到 Secret（myapp-tls-cert）
    ↓

11. 证书就绪
    ↓
    Certificate Ready=True
    ↓

12. Ingress 自动使用证书
    ↓
    用户访问 https://www.example.com
    ↓

13. 过期前 30 天自动续期
    ↓
    重复步骤 4-12
```

---

## 4. ACME 协议简介

### 4.1 什么是 ACME？

**ACME** (Automatic Certificate Management Environment) 是一个自动化证书管理协议，由 Let's Encrypt 开发。

**作用**:
- 自动化证书申请、验证、签发、续期
- 无需人工干预

**实现**:
- Let's Encrypt
- ZeroSSL
- 其他 ACME 服务器

---

### 4.2 ACME 工作流程

```
客户端（cert-manager）          ACME 服务器（Let's Encrypt）
      │                                  │
      │  1. 注册账户                      │
      ├─────────────────────────────────>│
      │                                  │
      │  2. 返回账户 URL                  │
      │<─────────────────────────────────┤
      │                                  │
      │  3. 提交 CSR（证书签名请求）       │
      ├─────────────────────────────────>│
      │                                  │
      │  4. 返回挑战（HTTP-01/DNS-01）    │
      │<─────────────────────────────────┤
      │                                  │
      │  5. 完成挑战                      │
      ├─────────────────────────────────>│
      │  - HTTP-01: 放置文件到 Web 服务器 │
      │  - DNS-01: 配置 DNS TXT 记录     │
      │                                  │
      │  6. ACME 服务器验证挑战           │
      │  - HTTP-01: 访问验证 URL          │
      │  - DNS-01: 查询 DNS TXT 记录     │
      │                                  │
      │  7. 验证成功，签发证书             │
      │<─────────────────────────────────┤
      │                                  │
      │  8. 下载证书                      │
      │<─────────────────────────────────┤
```

---

### 4.3 HTTP-01 验证

**原理**: 通过 HTTP 访问验证域名所有权

**流程**:
```
1. ACME 服务器返回挑战
   - Token: 随机字符串
   - 要求: 在 http://domain/.well-known/acme-challenge/<token> 放置 Key

2. cert-manager 完成挑战
   - 创建临时 Pod
   - 创建临时 Ingress
   - 配置路由: /.well-known/acme-challenge/<token> → 临时 Pod
   - 临时 Pod 返回: <token>.<key>

3. ACME 服务器验证
   - 访问: http://domain/.well-known/acme-challenge/<token>
   - 验证返回值是否匹配
```

**要求**:
- ✅ 域名已解析到服务器 IP
- ✅ 80 端口可从公网访问
- ✅ Ingress Controller 已运行

**优点**:
- ✅ 配置简单
- ✅ 无需 DNS API 凭证

**缺点**:
- ❌ 需要 80 端口
- ❌ 不支持通配符证书

---

### 4.4 DNS-01 验证

**原理**: 通过 DNS TXT 记录验证域名所有权

**流程**:
```
1. ACME 服务器返回挑战
   - Token: 随机字符串
   - 要求: 在 _acme-challenge.domain 配置 TXT 记录

2. cert-manager 完成挑战
   - 调用 DNS API（阿里云/Cloudflare）
   - 创建 TXT 记录: _acme-challenge.domain → <key>
   - 等待 DNS 生效（1-5 分钟）

3. ACME 服务器验证
   - 查询: _acme-challenge.domain TXT
   - 验证记录值是否匹配
```

**要求**:
- ✅ DNS API 访问权限
- ✅ DNS API 凭证（Access Key/Secret）

**优点**:
- ✅ 支持通配符证书（*.example.com）
- ✅ 无需 80 端口
- ✅ 可以申请多个域名的证书

**缺点**:
- ❌ 需要配置 DNS API 凭证
- ❌ DNS 生效较慢（1-5 分钟）

---

## 5. 证书来源

### 5.1 Let's Encrypt（免费）

**特点**:
- ✅ 免费
- ✅ 自动化（ACME）
- ✅ 受信任（主流浏览器信任）
- ✅ 有效期 90 天

**适用场景**:
- 个人网站
- 博客
- 小型应用

**限制**:
- ❌ 速率限制（每周 5 个失败证书）
- ❌ 不提供商业担保

---

### 5.2 商业证书（付费）

**提供商**:
- DigiCert
- GlobalSign
- Sectigo

**特点**:
- ✅ 商业担保
- ✅ 更高信誉度
- ✅ 支持组织验证（OV）、扩展验证（EV）

**适用场景**:
- 企业应用
- 电商平台
- 金融服务

**成本**:
- ¥200-10000/年

---

### 5.3 自签名证书（内部）

**特点**:
- ✅ 免费
- ✅ 无需外部依赖
- ✅ 完全控制

**适用场景**:
- 内部应用
- 开发环境
- 测试环境

**缺点**:
- ❌ 浏览器不信任（需手动添加例外）
- ❌ 不适合公网访问

---

### 5.4 内部 CA（企业）

**特点**:
- ✅ 企业自主控制
- ✅ 统一管理
- ✅ 可自动化

**适用场景**:
- 大型企业
- 内部系统

**要求**:
- 部署内部 CA 服务器
- 在客户端安装根证书

---

## 6. 应用场景

### 场景 1: 个人网站

```
需求:
- 域名: www.example.com
- 证书: Let's Encrypt（免费）
- 验证方式: HTTP-01

解决方案:
┌─────────────────────────────────┐
│ ClusterIssuer (Let's Encrypt)   │
├─────────────────────────────────┤
│ spec:                           │
│   acme:                         │
│     server: letsencrypt.org     │
│     solvers:                    │
│     - http01:                   │
│         ingress:                │
│           class: nginx          │
└─────────────────────────────────┘
            ↓
┌─────────────────────────────────┐
│ Certificate                     │
├─────────────────────────────────┤
│ spec:                           │
│   dnsNames:                     │
│   - www.example.com             │
│   secretName: myapp-tls-cert    │
└─────────────────────────────────┘
            ↓
┌─────────────────────────────────┐
│ Ingress                         │
├─────────────────────────────────┤
│ annotations:                    │
│   cert-manager.io/cluster-issuer│
│     : letsencrypt-prod          │
│ tls:                            │
│ - secretName: myapp-tls-cert    │
└─────────────────────────────────┘
```

---

### 场景 2: 通配符证书

```
需求:
- 域名: *.example.com
- 证书: Let's Encrypt（免费）
- 验证方式: DNS-01（支持通配符）

解决方案:
┌─────────────────────────────────┐
│ ClusterIssuer (Let's Encrypt)   │
├─────────────────────────────────┤
│ spec:                           │
│   acme:                         │
│     solvers:                    │
│     - dns01:                    │
│         cloudflare:             │
│           email: xxx@example.com│
│           apiTokenSecretRef:    │
│             name: cloudflare-api│
└─────────────────────────────────┘
            ↓
┌─────────────────────────────────┐
│ Certificate                     │
├─────────────────────────────────┤
│ spec:                           │
│   dnsNames:                     │
│   - "*.example.com"             │
│   - example.com                 │
└─────────────────────────────────┘
```

---

### 场景 3: 企业内部应用

```
需求:
- 域名: app.internal
- 证书: 自签名 CA
- 验证方式: 自签名

解决方案:
┌─────────────────────────────────┐
│ ClusterIssuer (Self-Signed CA)  │
├─────────────────────────────────┤
│ spec:                           │
│   selfSigned: {}                │
└─────────────────────────────────┘
            ↓
┌─────────────────────────────────┐
│ Certificate                     │
├─────────────────────────────────┤
│ spec:                           │
│   dnsNames:                     │
│   - app.internal                │
│   - "*.app.internal"            │
└─────────────────────────────────┘
```

---

## 7. 与 Kubernetes 的集成

### 7.1 Ingress 集成

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    # cert-manager 注解（自动签发证书）
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    secretName: myapp-tls-cert  # cert-manager 自动创建
  rules:
  - host: www.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp-service
            port:
              number: 80
```

**工作流程**:
```
1. 创建 Ingress
   ↓
2. cert-manager 监听到 Ingress 创建
   ↓
3. 检测到 cert-manager.io/cluster-issuer 注解
   ↓
4. 自动创建 Certificate 资源
   ↓
5. 自动签发证书
   ↓
6. 自动创建 Secret: myapp-tls-cert
   ↓
7. Ingress 自动使用证书
```

---

### 7.2 Gateway API 集成

```yaml
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: myapp-gateway
spec:
  gatewayClassName: nginx
  listeners:
  - name: https
    protocol: HTTPS
    port: 443
    hostname: www.example.com
    tls:
      mode: Terminate
      certificateRefs:
      - kind: Secret
        name: myapp-tls-cert  # cert-manager 自动创建
      options:
        cert-manager:
          issuerRef:
            name: letsencrypt-prod
            kind: ClusterIssuer
```

---

### 7.3 监控集成

```bash
# cert-manager 提供 Prometheus 指标
- certificate_ready_status
- certificate_expiration_timestamp_seconds
- certificate_renewal_timestamp_seconds
- acme_client_request_count
- acme_client_request_duration_seconds

# 可以接入 Prometheus + Grafana
```

---

## 总结

### 关键要点

1. **cert-manager** 是 Kubernetes 原生的证书自动化管理工具
2. **核心资源**: Issuer/ClusterIssuer、Certificate、CertificateRequest
3. **工作流程**: 监听 Certificate → ACME 协议 → 自动签发 → 保存到 Secret
4. **验证方式**: HTTP-01（简单）、DNS-01（支持通配符）
5. **证书来源**: Let's Encrypt（免费）、商业证书（付费）、自签名（内部）

### 学习建议

```
理解概念 → 阅读本部分（01-what-is-certmanager.md）
    ↓
理解原理 → 阅读下一部分（02-how-it-works.md）
    ↓
安装部署 → 跟随安装指南（03-installation.md）
    ↓
配置实战 → 查看配置指南（04-configuration.md）
    ↓
解决问题 → 参考故障排查（05-troubleshooting.md）
```

---

**下一步**: [02-how-it-works.md](./02-how-it-works.md) 🚀
