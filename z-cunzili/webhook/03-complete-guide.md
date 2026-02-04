# Kubernetes Ingress 生产环境部署指南

> 从本地学习到生产环境的完整方案，包含云厂商配置、域名、HTTPS 和完整示例

**作者**: cunzili
**版本**: v2.0
**更新日期**: 2025-01-15

**前置知识**: 建议先阅读 [01-concepts.md](./01-concepts.md) 和 [02-quick-start.md](./02-quick-start.md)

---

## 📋 目录

- [1. 生产环境方案对比](#1-生产环境方案对比)
- [2. 云厂商 LoadBalancer 配置](#2-云厂商-loadbalancer-配置)
- [3. 域名和 DNS 配置](#3-域名和-dns-配置)
- [4. HTTPS 证书配置](#4-https-证书配置)
- [5. 完整生产示例](#5-完整生产示例)
- [6. 高可用配置](#6-高可用配置)
- [7. 监控和日志](#7-监控和日志)

---

## 1. 生产环境方案对比

### 1.1 方案对比表

| 方案 | 成本/月 | 复杂度 | 高可用 | 适用场景 | 推荐度 |
|------|---------|--------|--------|----------|--------|
| **云厂商 LoadBalancer** | $15-30 | 低 | 高 | 生产环境 | ⭐⭐⭐⭐⭐ |
| **NodePort + 反向代理** | $5-10 + 带宽 | 中 | 低 | 小型应用 | ⭐⭐⭐ |
| **MetalLB + BGP** | 服务器成本 | 高 | 高 | 企业/IDC | ⭐⭐⭐⭐ |
| **CDN + 源站** | 免费-$50 | 中 | 高 | 全球加速 | ⭐⭐⭐⭐ |

### 1.2 详细分析

#### 方案 A: 云厂商 LoadBalancer（推荐）

```
优点:
✅ 自动分配公网 IP
✅ DDoS 防护
✅ SSL 卸载（可选）
✅ 全球加速
✅ 高可用
✅ 运维成本低

缺点:
❌ 成本较高（$15-30/月）
❌ 依赖云厂商

适用: 生产环境、中型以上应用
```

#### 方案 B: NodePort + 反向代理

```
优点:
✅ 成本低
✅ 灵活控制

缺点:
❌ 需要手动管理反向代理
❌ 高可用配置复杂
❌ 扩展性差

适用: 小型应用、测试环境
```

#### 方案 C: MetalLB + BGP

```
优点:
✅ 完全控制
✅ 不依赖云厂商
✅ 真正的 LoadBalancer

缺点:
❌ 配置复杂
❌ 需要 BGP 路由器支持
❌ 运维成本高

适用: 企业 IDC、私有云
```

---

## 2. 云厂商 LoadBalancer 配置

### 2.1 AWS EKS 配置

#### 创建 LoadBalancer Service

```yaml
# ingress-nginx-lb.yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
  annotations:
    # 使用 NLB（网络负载均衡器）
    service.beta.kubernetes.io/aws-load-balancer-type: nlb

    # 或使用 ALB（应用负载均衡器）
    # service.beta.kubernetes.io/aws-load-balancer-type: alb

    # 公网访问
    service.beta.kubernetes.io/aws-load-balancer-scheme: internet-facing

    # 跨可用区
    service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"

    # SSL 证书（ARN）
    # service.beta.kubernetes.io/aws-load-balancer-ssl-cert: arn:aws:acm:...

    # 健康检查
    service.beta.kubernetes.io/aws-load-balancer-healthcheck-path: /healthz
    service.beta.kubernetes.io/aws-load-balancer-healthcheck-protocol: HTTP
    service.beta.kubernetes.io/aws-load-balancer-healthcheck-interval: "30"
    service.beta.kubernetes.io/aws-load-balancer-healthcheck-timeout: "5"
    service.beta.kubernetes.io/aws-load-balancer-healthy-threshold: "2"
    service.beta.kubernetes.io/aws-load-balancer-unhealthy-threshold: "2"

spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
  - name: https
    port: 443
    targetPort: 443
    protocol: TCP
```

```bash
# 应用配置
kubectl apply -f ingress-nginx-lb.yaml

# 查看 LoadBalancer
kubectl get svc ingress-nginx -n ingress-nginx

# 输出:
# NAME            TYPE           EXTERNAL-IP      PORT(S)
# ingress-nginx   LoadBalancer   xxx.us-east-1.elb.amazonaws.com   80:xxxxx/TCP,443:xxxxx/TCP
#                                         ↑
#                            AWS 自动分配的 DNS 名称（自动绑定公网 IP）

# 解析 DNS 获取公网 IP
dig +short xxx.us-east-1.elb.amazonaws.com
```

#### AWS 费用说明

```
成本构成:
1. NLB 费用: 约 $0.0225/小时 ≈ $16.2/月
2. LCU 费用: 约 $0.006/小时 ≈ $4.32/月
3. 流量费: 前 1GB 免费，之后 $0.008/GB

估算（小型应用）:
- 月费用: 约 $20-30/月
- 流量 100GB: +$0.8
- 总计: 约 $21-31/月
```

### 2.2 阿里云 ACK 配置

#### 创建 LoadBalancer Service

```yaml
# ingress-nginx-lb.yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
  annotations:
    # SLB 实例规格
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-spec: slb.s3.small

    # 计费方式（按流量计费）
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-charge-type: paybytraffic

    # 协议端口映射
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-protocol-port: "http:80,https:443"

    # 带宽峰值（1-5000 Mbps）
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-bandwidth: "10"

    # 负载均衡算法
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-scheduler: wrr

    # 健康检查
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-flag: "on"
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-type: http
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-health-check-uri: /healthz
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-healthy-threshold: "3"
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-unhealthy-threshold: "3"

    # 主备服务器组
    # service.beta.kubernetes.io/alibaba-cloud-loadbalancer-master-zoneid: cn-hangzhou-i
    # service.beta.kubernetes.io/alibaba-cloud-loadbalancer-slave-zoneid: cn-hangzhou-h

spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
  - name: https
    port: 443
    targetPort: 443
    protocol: TCP
```

```bash
# 应用配置
kubectl apply -f ingress-nginx-lb.yaml

# 查看 LoadBalancer
kubectl get svc ingress-nginx -n ingress-nginx

# 输出:
# NAME            TYPE           EXTERNAL-IP      PORT(S)
# ingress-nginx   LoadBalancer   47.96.123.45     80:xxxxx/TCP,443:xxxxx/TCP
#                                         ↑
#                            阿里云自动分配的公网 IP
```

#### 阿里云费用说明

```
成本构成:
1. SLB 实例费: 约 ¥0.02/小时 ≈ ¥14.4/月
2. 流量费: ¥0.42/GB（按流量计费）

估算（小型应用）:
- SLB 实例: ¥14.4/月
- 流量 100GB: ¥42
- 总计: 约 ¥56.4/月 ≈ $8/月

优化建议:
- 使用按带宽计费（带宽固定）
- 带宽 5Mbps: 约 ¥150/月（流量不限）
```

### 2.3 其他云厂商

#### 腾讯云 TKE

```yaml
annotations:
  # CLB 类型（公网）
  service.kubernetes.io/qcloud-loadbalancer-internet-charge-type: TRAFFIC_POSTPAID_BY_HOUR

  # 网络类型（公网）
  service.kubernetes.io/qcloud-loadbalancer-internet: "true"

  # 带宽
  service.kubernetes.io/qcloud-loadbalancer-bandwidth: "10"
```

#### Google Cloud GKE

```yaml
annotations:
  # 使用网络层级（Premium = 全球加速）
  networking.gke.io/load-balancer-type: "Internal"  # 或 "External"

  # 全局访问
  networking.gke.io/scope: " regional"  # 或 "global"
```

---

## 3. 域名和 DNS 配置

### 3.1 购买域名

```bash
# 推荐域名注册商
- 阿里云: https://wanwang.aliyun.com/
- 腾讯云: https://dnspod.cloud.tencent.com/
- Cloudflare: https://www.cloudflare.com/
- GoDaddy: https://www.godaddy.com/

# 域名选择建议
- 简短易记
- 避免特殊字符
- .com / .cn / .net 最常用
- 成本: 约 ¥50-100/年
```

### 3.2 配置 DNS 记录

#### 在域名服务商控制台配置

```
场景 1: 直接指向 LoadBalancer IP
─────────────────────────────────────
@ A 47.96.123.45         # example.com → 47.96.123.45
www A 47.96.123.45       # www.example.com → 47.96.123.45
api A 47.96.123.45       # api.example.com → 47.96.123.45

场景 2: 使用 CNAME（推荐 AWS）
─────────────────────────────────────
@ CNAME xxx.us-east-1.elb.amazonaws.com.
www CNAME xxx.us-east-1.elb.amazonaws.com.

场景 3: 使用 CDN（全球加速）
─────────────────────────────────────
@ CNAME xxx.cdn.cloudflare.net.
www CNAME xxx.cdn.cloudflare.net.
```

#### DNS 生效时间

```bash
# DNS 更新后需要等待生效
- 国际域名: 5分钟 - 48小时（通常几分钟）
- 国内域名: 10分钟 - 24小时（通常几小时）

# 测试 DNS 是否生效
dig +short www.example.com
nslookup www.example.com

# 强制刷新本地 DNS（开发测试）
# macOS
sudo dscacheutil -flushcache && sudo killall -HUP mDNSResponder

# Linux
sudo systemd-resolve --flush-caches

# Windows
ipconfig /flushdns
```

### 3.3 DNS 配置最佳实践

```
1. 使用 TTL 控制缓存时间
   - 默认 TTL: 600（10分钟）
   - 生产环境: 300-600
   - 修改 IP 前调低: 60

2. 使用 CNAME 代替 A 记录（云环境）
   - 优点: IP 变化无需更新 DNS
   - 缺点: 需要云厂商支持

3. 配置多个记录（高可用）
   - 主记录: example.com A 1.2.3.4
   - 备用记录: example.com A 5.6.7.8

4. 使用 DNS 服务商
   - Cloudflare（免费）
   - 阿里云 DNS
   - 腾讯云 DNSPod
```

---

## 4. HTTPS 证书配置

### 4.1 证书类型对比

| 类型 | 成本 | 信任度 | 适用场景 | 推荐度 |
|------|------|--------|----------|--------|
| **Let's Encrypt** | 免费 | 高 | 个人/小型项目 | ⭐⭐⭐⭐⭐ |
| **云厂商证书** | ¥200-2000/年 | 高 | 生产环境 | ⭐⭐⭐⭐ |
| **DigiCert** | ¥2000-10000/年 | 极高 | 企业/金融 | ⭐⭐⭐ |

### 4.2 使用 cert-manager 自动签发（推荐）

#### 安装 cert-manager

```bash
# 安装 cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# 验证安装
kubectl get pods -n cert-manager

# 预期输出:
# NAME                                       READY   STATUS    AGE
# cert-manager-xxx                           1/1     Running   1m
# cert-manager-cainjector-xxx                1/1     Running   1m
# cert-manager-webhook-xxx                   1/1     Running   1m
```

#### 创建 ClusterIssuer（Let's Encrypt）

```yaml
# letsencrypt-clusterissuer.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    # Let's Encrypt 生产环境服务器
    server: https://acme-v02.api.letsencrypt.org/directory

    # 你的邮箱（证书过期提醒）
    email: admin@example.com

    # 私钥存储
    privateKeySecretRef:
      name: letsencrypt-prod

    # HTTP-01 验证方式（推荐）
    solvers:
    - http01:
        ingress:
          class: nginx
```

```bash
# 应用配置
kubectl apply -f letsencrypt-clusterissuer.yaml

# 验证
kubectl get clusterissuer

# 输出:
# NAME               READY   AGE
# letsencrypt-prod   True    1m
```

#### 在 Ingress 中启用 TLS

```yaml
# myapp-ingress-tls.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    # 指定 ClusterIssuer
    cert-manager.io/cluster-issuer: letsencrypt-prod

    # 强制 HTTPS（可选）
    nginx.ingress.kubernetes.io/ssl-redirect: "true"

    # HSTS（可选，增强安全性）
    nginx.ingress.kubernetes.io/hsts: "true"
    nginx.ingress.kubernetes.io/hsts-max-age: "31536000"
    nginx.ingress.kubernetes.io/hsts-include-subdomains: "true"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    - example.com
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
kubectl get certificate

# 输出:
# NAME          READY   SECRET         AGE
# myapp-tls     True    myapp-tls-cert  2m

# 查看证书详情
kubectl describe certificate myapp-tls

# 输出:
# Status:
#   Conditions:
#     Type:    Ready
#     Status:  True
#     Message: Certificate is up to date and has not expired
```

#### 测试 HTTPS 访问

```bash
# 浏览器访问
https://www.example.com/

# 命令行测试
curl -I https://www.example.com/

# 检查证书
curl -vI https://www.example.com/ 2>&1 | grep -E "subject|issuer"
# subject: CN=www.example.com
# issuer: C=US; O=Let's Encrypt; CN=R3

# 测试 SSL 配置
openssl s_client -connect www.example.com:443 -servername www.example.com
```

### 4.3 使用云厂商证书

#### 购买和上传证书

```bash
# 1. 购买证书（阿里云为例）
# 登录阿里云控制台 → SSL 证书 → 购买证书
# 成本: DV（域名验证）证书约 ¥200/年

# 2. 下载证书文件
# - xxx.key  # 私钥
# - xxx.pem  # 证书

# 3. 创建 Secret
kubectl create secret tls myapp-tls-cert \
  --cert=xxx.pem \
  --key=xxx.key

# 4. 在 Ingress 中使用
```

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    secretName: myapp-tls-cert  # 使用云厂商证书
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

---

## 5. 完整生产示例

### 5.1 架构设计

```
                        用户
                          │
                          ▼ DNS: www.example.com
                    ┌─────────────┐
                    │ 云 CDN（可选）│
                    │  - 全球加速   │
                    │  - DDoS 防护 │
                    └─────────────┘
                          │
                          ▼
              ┌───────────────────────┐
              │ 云厂商 LoadBalancer    │
              │  公网 IP: 47.96.123.45 │
              │  - SSL 卸载（可选）     │
              │  - 健康检查            │
              └───────────────────────┘
                          │
                          ▼
              ┌───────────────────────┐
              │ Kubernetes 集群       │
              │                        │
              │  ┌─────────────────┐  │
              │  │ Ingress Controller│ │
              │  │ (Nginx)         │  │
              │  └─────────────────┘  │
              │           │            │
              │  ┌─────────────────┐  │
              │  │ Service         │  │
              │  │ ClusterIP       │  │
              │  └─────────────────┘  │
              │           │            │
              │  ┌─────────────────┐  │
              │  │ Pods (×3)       │  │
              │  │ - app-xxx-xxx   │  │
              │  │ - app-xxx-yyy   │  │
              │  │ - app-xxx-zzz   │  │
              │  └─────────────────┘  │
              └───────────────────────┘
```

### 5.2 完整部署步骤

#### 步骤 1: 准备工作

```bash
# 1. 购买域名
# example.com

# 2. 购买云服务器（或使用托管 K8s）
# 阿里云 ECS: 2核4G，5Mbps 带宽
# 或直接使用阿里云 ACK（托管 K8s）

# 3. 部署 Kubernetes
# 使用 sealos/kubeadm/rancher
# 或直接使用托管 K8s（EKS/ACK）
```

#### 步骤 2: 部署应用

```yaml
# deployment-production.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: production
  labels:
    app: myapp
    version: v1.0.0
spec:
  replicas: 3  # 生产环境至少 3 个副本
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
        version: v1.0.0
    spec:
      # 反亲和性（Pod 分散到不同节点）
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - myapp
              topologyKey: kubernetes.io/hostname
      containers:
      - name: myapp
        image: nginx:1.25
        ports:
        - containerPort: 80
          name: http
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
        livenessProbe:
          httpGet:
            path: /healthz
            port: http
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /ready
            port: http
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 3
```

```bash
# 创建 Namespace
kubectl create namespace production

# 部署应用
kubectl apply -f deployment-production.yaml

# 验证
kubectl get deployment myapp -n production
kubectl get pods -l app=myapp -n production
```

#### 步骤 3: 创建 Service

```yaml
# service-production.yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
  namespace: production
  labels:
    app: myapp
spec:
  selector:
    app: myapp
  ports:
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
  type: ClusterIP
```

```bash
# 应用配置
kubectl apply -f service-production.yaml

# 验证
kubectl get svc myapp-service -n production
kubectl get endpoints myapp-service -n production
```

#### 步骤 4: 安装 Ingress Controller

```bash
# 安装 ingress-nginx
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml

# 等待就绪
kubectl wait --for=condition=available \
  deployment/ingress-nginx-controller \
  -n ingress-nginx \
  --timeout=300s

# 验证
kubectl get pods -n ingress-nginx
```

#### 步骤 5: 创建 LoadBalancer

```yaml
# ingress-nginx-lb.yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx-controller
  namespace: ingress-nginx
  annotations:
    # 阿里云配置
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-spec: slb.s3.small
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-charge-type: paybytraffic
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-protocol-port: "http:80,https:443"
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-bandwidth: "10"
spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
  - name: https
    port: 443
    targetPort: 443
    protocol: TCP
```

```bash
# 应用配置
kubectl apply -f ingress-nginx-lb.yaml

# 获取公网 IP
kubectl get svc ingress-nginx-controller -n ingress-nginx

# 输出:
# NAME                        TYPE           EXTERNAL-IP      PORT(S)
# ingress-nginx-controller    LoadBalancer   47.96.123.45     80:xxxxx/TCP,443:xxxxx/TCP

# 记录公网 IP（配置 DNS 用）
LB_IP=47.96.123.45
```

#### 步骤 6: 配置 DNS

```bash
# 在域名服务商控制台（阿里云DNS）

# 添加记录:
@ A 47.96.123.45           # example.com → 47.96.123.45
www A 47.96.123.45         # www.example.com → 47.96.123.45
api A 47.96.123.45         # api.example.com → 47.96.123.45

# 等待 DNS 生效（几分钟到几小时）
dig +short www.example.com
```

#### 步骤 7: 安装 cert-manager

```bash
# 安装
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# 等待就绪
kubectl wait --for=condition=available \
  deployment/cert-manager \
  -n cert-manager \
  --timeout=300s
```

#### 步骤 8: 创建 ClusterIssuer

```yaml
# clusterissuer.yaml
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
# 应用配置
kubectl apply -f clusterissuer.yaml

# 验证
kubectl get clusterissuer
```

#### 步骤 9: 创建 Ingress（启用 TLS）

```yaml
# ingress-production.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  namespace: production
  annotations:
    # cert-manager
    cert-manager.io/cluster-issuer: letsencrypt-prod

    # nginx.ingress.kubernetes.io
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    nginx.ingress.kubernetes.io/hsts: "true"
    nginx.ingress.kubernetes.io/hsts-max-age: "31536000"

    # 安全头部
    nginx.ingress.kubernetes.io/configuration-snippet: |
      add_header X-Frame-Options "SAMEORIGIN" always;
      add_header X-Content-Type-Options "nosniff" always;
      add_header X-XSS-Protection "1; mode=block" always;

spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    - example.com
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
  - host: example.com
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
kubectl apply -f ingress-production.yaml

# 查看证书签发状态
kubectl get certificate -n production

# 输出:
# NAME          READY   SECRET           AGE
# myapp-tls     True    myapp-tls-cert   2m

# 查看 Ingress
kubectl get ingress myapp-ingress -n production

# 输出:
# NAME            CLASS   HOSTS                  ADDRESS          PORTS     AGE
# myapp-ingress   nginx   www.example.com        47.96.123.45     80, 443   5m
#                         example.com
```

#### 步骤 10: 验证

```bash
# 1. 测试 HTTP（应该重定向到 HTTPS）
curl -I http://www.example.com/

# 输出:
# HTTP/1.1 308 Permanent Redirect
# Location: https://www.example.com/

# 2. 测试 HTTPS
curl -I https://www.example.com/

# 输出:
# HTTP/2 200
# content-type: text/html
# strict-transport-security: max-age=31536000; includeSubDomains

# 3. 检查证书
curl -vI https://www.example.com/ 2>&1 | grep -E "subject|issuer"
# subject: CN=www.example.com
# issuer: C=US; O=Let's Encrypt; CN=R3

# 4. 浏览器访问
# https://www.example.com/
# 应该看到 HTTPS 锁图标
```

---

## 6. 高可用配置

### 6.1 多副本部署

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: production
spec:
  replicas: 3  # 至少 3 个副本
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1        # 滚动更新时最多多 1 个 Pod
      maxUnavailable: 0  # 滚动更新时最多 0 个 Pod 不可用
```

### 6.2 Pod 反亲和性（分散到不同节点）

```yaml
spec:
  affinity:
    podAntiAffinity:
      # 尽量分散（软要求）
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchExpressions:
            - key: app
              operator: In
              values:
              - myapp
          topologyKey: kubernetes.io/hostname

      # 强制分散（硬要求）
      # requiredDuringSchedulingIgnoredDuringExecution:
      # - labelSelector:
      #     matchExpressions:
      #     - key: app
      #       operator: In
      #       values:
      #       - myapp
      #   topologyKey: kubernetes.io/hostname
```

### 6.3 健康检查

```yaml
containers:
- name: myapp
  image: nginx:1.25
  livenessProbe:
    httpGet:
      path: /healthz
      port: http
    initialDelaySeconds: 30
    periodSeconds: 10
    timeoutSeconds: 5
    failureThreshold: 3  # 连续失败 3 次重启 Pod
  readinessProbe:
    httpGet:
      path: /ready
      port: http
    initialDelaySeconds: 10
    periodSeconds: 5
    timeoutSeconds: 3
    failureThreshold: 3  # 连续失败 3 次移出 Service
```

### 6.4 资源限制

```yaml
containers:
- name: myapp
  resources:
    requests:
      cpu: 100m      # 0.1 CPU（保证）
      memory: 128Mi  # 128MB（保证）
    limits:
      cpu: 500m      # 0.5 CPU（上限）
      memory: 512Mi  # 512MB（上限）
```

---

## 7. 监控和日志

### 7.1 Ingress Controller 日志

```bash
# 查看日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx --tail=100 -f

# 查看特定请求
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx | grep "www.example.com"
```

### 7.2 Prometheus + Grafana（推荐）

```bash
# 安装 Prometheus Operator
kubectl apply -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/bundle.yaml

# 安装 Grafana
helm repo add grafana https://grafana.github.io/helm-charts
helm install grafana grafana/grafana -n monitoring

# 导入 Ingress Dashboard
# Dashboard ID: 9614 (NGINX Ingress Controller)
```

### 7.3 日志聚合（ELK/Loki）

```bash
# 安装 Loki（轻量级日志聚合）
helm repo add grafana https://grafana.github.io/helm-charts
helm install loki grafana/loki-stack -n monitoring

# 查询日志
logcli --addr=http://loki.monitoring.svc.cluster.local:3100 query '{app="ingress-nginx"}'
```

---

## 总结

### 生产环境检查清单

```bash
#!/bin/bash

echo "=== 生产环境检查清单 ==="

# 1. 副本数
echo -e "\n1. Pod 副本数"
kubectl get deployment -n production

# 2. 端点
echo -e "\n2. Endpoints"
kubectl get endpoints -n production

# 3. Ingress
echo -e "\n3. Ingress"
kubectl get ingress -n production

# 4. 证书
echo -e "\n4. 证书状态"
kubectl get certificate -n production

# 5. LoadBalancer
echo -e "\n5. LoadBalancer"
kubectl get svc ingress-nginx-controller -n ingress-nginx

# 6. HTTPS 测试
echo -e "\n6. HTTPS 测试"
curl -I https://www.example.com/ 2>/dev/null | head -1

# 7. DNS 解析
echo -e "\n7. DNS 解析"
dig +short www.example.com

echo -e "\n=== 检查完成 ==="
```

### 成本优化建议

```
1. 使用预付费实例（节省 30-50%）
2. 按带宽计费 vs 按流量计费（根据实际情况选择）
3. 使用 CDN 节省流量费
4. 使用 Spot/Preemptible 实例（非关键应用）
5. 定期清理未使用的资源
```

---

**下一步**: [04-troubleshooting.md](./04-troubleshooting.md) 或返回 [02-quick-start.md](./02-quick-start.md)

**祝你部署顺利！** 🚀
