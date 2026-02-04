# cert-manager 配置实战

> 从 Let's Encrypt 到生产环境的完整配置指南

**作者**: cunzili
**版本**: v1.0
**更新日期**: 2025-01-15

---

## 📋 目录

- [1. 快速开始（HTTP-01 验证）](#1-快速开始http-01-验证)
- [2. ClusterIssuer 配置详解](#2-clusterissuer-配置详解)
- [3. Certificate 配置详解](#3-certificate-配置详解)
- [4. Ingress 集成](#4-ingress-集成)
- [5. DNS-01 验证配置](#5-dns-01-验证配置)
- [6. 完整生产示例](#6-完整生产示例)
- [7. 证书续期验证](#7-证书续期验证)

---

## 1. 快速开始（HTTP-01 验证）

### 1.1 前置条件

```bash
✅ Kubernetes 集群运行正常
✅ Ingress Controller 已安装（如 nginx-ingress）
✅ 域名已解析到 LoadBalancer IP 或节点 IP
✅ 80 端口可从公网访问
```

### 1.2 创建 ClusterIssuer

```yaml
# letsencrypt-clusterissuer.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    # Let's Encrypt ACME 服务器
    server: https://acme-v02.api.letsencrypt.org/directory

    # 你的邮箱（证书过期提醒）
    email: admin@example.com

    # 私钥存储
    privateKeySecretRef:
      name: letsencrypt-prod

    # HTTP-01 验证配置
    solvers:
    - http01:
        ingress:
          class: nginx  # Ingress Class
```

```bash
# 应用配置
kubectl apply -f letsencrypt-clusterissuer.yaml

# 验证
kubectl get clusterissuer letsencrypt-prod

# 输出:
# NAME              READY   AGE
# letsencrypt-prod   True    1m
```

### 1.3 创建 Ingress（自动签发证书）

```yaml
# myapp-ingress-tls.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  namespace: default
  annotations:
    # cert-manager 注解（自动签发证书）
    cert-manager.io/cluster-issuer: letsencrypt-prod

    # nginx.ingress.kubernetes.io 注解（可选）
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
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

```bash
# 应用配置
kubectl apply -f myapp-ingress-tls.yaml

# 查看证书签发状态
kubectl get certificate -n default

# 输出:
# NAME          READY   SECRET           AGE
# myapp-tls     True    myapp-tls-cert   2m

# 查看详情
kubectl describe certificate myapp-tls -n default

# Events:
# Normal  Issuing     2m   cert-manager  Issuing certificate...
# Normal  Generated   2m   cert-manager  Generated new private key
# Normal  Requested   2m   cert-manager  Created new CertificateRequest
# Normal  Issued      1m   cert-manager  Certificate issued successfully
```

---

## 2. ClusterIssuer 配置详解

### 2.1 Let's Encrypt 生产环境

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    # 生产环境服务器
    server: https://acme-v02.api.letsencrypt.org/directory

    # 邮箱
    email: admin@example.com

    # 私钥存储
    privateKeySecretRef:
      name: letsencrypt-prod

    # HTTP-01 验证
    solvers:
    - http01:
        ingress:
          class: nginx
```

### 2.2 Let's Encrypt 暂存环境

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    # 暂存环境服务器（速率限制更宽松）
    server: https://acme-staging-v02.api.letsencrypt.org/directory

    email: admin@example.com

    privateKeySecretRef:
      name: letsencrypt-staging

    solvers:
    - http01:
        ingress:
          class: nginx
```

**建议**: 先使用暂存环境测试，配置正确后再切换到生产环境

---

## 3. Certificate 配置详解

### 3.1 基本配置

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: myapp-tls
  namespace: production
spec:
  # Secret 名称
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
  duration: 2160h  # 90天

  # 续期时间（过期前 30 天）
  renewBefore: 720h  # 30天

  # 证书算法（默认 RSA 2048）
  keyAlgorithm: rsa
  keySize: 2048

  # 或者使用 ECDSA
  # keyAlgorithm: ecdsa
  # keySize: 256
```

### 3.2 通配符证书

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: wildcard-tls
  namespace: production
spec:
  secretName: wildcard-tls-cert
  issuerRef:
    name: letsencrypt-dns  # 必须使用 DNS-01
    kind: ClusterIssuer
  dnsNames:
  - "*.example.com"  # 通配符
  - "example.com"
```

**注意**: 通配符证书必须使用 DNS-01 验证

---

## 4. Ingress 集成

### 4.1 基本 Ingress 配置

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    # cert-manager 注解
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    secretName: myapp-tls-cert
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

### 4.2 多域名 Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: multi-domain-ingress
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    - api.example.com
    secretName: multi-domain-tls-cert
  rules:
  - host: www.example.com
    http:
      paths:
      - backend:
          service:
            name: web-service
            port:
              number: 80
  - host: api.example.com
    http:
      paths:
      - backend:
          service:
            name: api-service
            port:
              number: 80
```

### 4.3 可选注解

```yaml
metadata:
  annotations:
    # cert-manager 注解
    cert-manager.io/cluster-issuer: letsencrypt-prod

    # nginx.ingress.kubernetes.io 注解
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    nginx.ingress.kubernetes.io/force-ssl-redirect: "true"

    # HSTS（HTTP Strict Transport Security）
    nginx.ingress.kubernetes.io/hsts: "true"
    nginx.ingress.kubernetes.io/hsts-max-age: "31536000"
    nginx.ingress.kubernetes.io/hsts-include-subdomains: "true"
```

---

## 5. DNS-01 验证配置

### 5.1 Cloudflare 配置

#### 创建 API Token Secret

```bash
# 在 Cloudflare 创建 API Token
# 权限: Zone - DNS - Edit
# 资源: Include - Specific zone - example.com

# 创建 Secret
kubectl create secret generic cloudflare-api-token \
  --from-literal=api-token=YOUR_CLOUDFLARE_API_TOKEN \
  -n cert-manager
```

#### 创建 ClusterIssuer

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-dns
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-dns
    solvers:
    - dns01:
        cloudflare:
          email: cloudflare@example.com
          apiTokenSecretRef:
            name: cloudflare-api-token
            key: api-token
```

---

### 5.2 阿里云 DNS 配置

#### 创建 Access Key Secret

```bash
# 获取阿里云 Access Key 和 Secret Key
# 创建 Secret
kubectl create secret generic alidns-secret \
  --from-literal=access-key=YOUR_ACCESS_KEY \
  --from-literal=secret-key=YOUR_SECRET_KEY \
  -n cert-manager
```

#### 创建 ClusterIssuer

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-dns
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-dns
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

---

## 6. 完整生产示例

### 6.1 架构

```
用户
  │
  ▼ DNS: www.example.com → LoadBalancer IP
┌───────────────────────────────────────┐
│ LoadBalancer (47.96.123.45)           │
└───────────────────────────────────────┘
  │
  ▼
┌───────────────────────────────────────┐
│ Kubernetes 集群                        │
│                                        │
│  ┌─────────────────────────────────┐  │
│  │ Ingress Controller (Nginx)      │  │
│  └─────────────────────────────────┘  │
│           │                            │
│  ┌─────────────────────────────────┐  │
│  │ Service (myapp-service)         │  │
│  └─────────────────────────────────┘  │
│           │                            │
│  ┌─────────────────────────────────┐  │
│  │ Pods (×3)                       │  │
│  │ - app-xxx-xxx                   │  │
│  │ - app-xxx-yyy                   │  │
│  │ - app-xxx-zzz                   │  │
│  └─────────────────────────────────┘  │
└───────────────────────────────────────┘
```

### 6.2 部署步骤

#### 步骤 1: 创建 ClusterIssuer

```yaml
# clusterissuer-prod.yaml
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
```

```bash
kubectl apply -f clusterissuer-prod.yaml
```

#### 步骤 2: 部署应用

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: production
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
      - name: myapp
        image: nginx:1.25
        ports:
        - containerPort: 80
```

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
  namespace: production
spec:
  selector:
    app: myapp
  ports:
  - port: 80
    targetPort: 80
  type: ClusterIP
```

```bash
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
```

#### 步骤 3: 创建 Ingress（启用 HTTPS）

```yaml
# ingress-prod.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  namespace: production
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    nginx.ingress.kubernetes.io/hsts: "true"
    nginx.ingress.kubernetes.io/hsts-max-age: "31536000"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    secretName: myapp-tls-cert
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

```bash
kubectl apply -f ingress-prod.yaml
```

#### 步骤 4: 验证

```bash
# 1. 检查证书状态
kubectl get certificate -n production

# 2. 检查 Secret
kubectl get secret myapp-tls-cert -n production

# 3. 检查 Ingress
kubectl get ingress -n production

# 4. 测试 HTTPS 访问
curl -I https://www.example.com/

# 5. 检查证书
openssl s_client -connect www.example.com:443 -servername www.example.com
```

---

## 7. 证书续期验证

### 7.1 查看证书过期时间

```bash
# 从 Secret 中查看证书
kubectl get secret myapp-tls-cert -n production -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -dates

# 输出:
# notBefore=Jan 15 00:00:00 2025 GMT
# notAfter=Apr 15 00:00:00 2025 GMT
```

### 7.2 查看续期状态

```bash
# 查看 Certificate 状态
kubectl get certificate myapp-tls -n production -o yaml

# 输出:
# status:
#   conditions:
#   - type: Ready
#     status: "True"
#     lastTransitionTime: "2025-01-15T10:00:00Z"
#   lastRenewalTime: "2025-01-15T10:00:00Z"
#   notAfter: "2025-04-15T00:00:00Z"
#   renewalTime: "2025-03-16T00:00:00Z"  # 过期前 30 天续期
```

### 7.3 手动触发续期

```bash
# 方法 1: 删除 Secret（不推荐，仅测试）
kubectl delete secret myapp-tls-cert -n production

# cert-manager 会自动重新申请证书

# 方法 2: 删除 CertificateRequest
kubectl get certificaterequest -n production
kubectl delete certificaterequest <request-name> -n production
```

---

## 总结

### 配置检查清单

```bash
✅ ClusterIssuer 创建成功
✅ 证书签发成功（Ready=True）
✅ Secret 创建成功
✅ Ingress 引用 Secret 正确
✅ HTTPS 访问正常
✅ 证书有效期正确（90天）
✅ 证书续期时间正确（过期前 30 天）
```

### 常见错误

| 错误 | 原因 | 解决 |
|------|------|------|
| **Ready=False** | 证书签发失败 | 查看 describe certificate |
| **HTTP-01 验证失败** | 域名未解析 / 80 端口不可访问 | 检查 DNS 和防火墙 |
| **DNS-01 验证失败** | DNS API 凭证错误 | 检查 Secret |
| **证书过期** | 自动续期失败 | 查看 cert-manager 日志 |

---

**下一步**: [05-troubleshooting.md](./05-troubleshooting.md) 🚀
