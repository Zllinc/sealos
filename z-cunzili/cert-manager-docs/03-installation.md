# cert-manager 安装指南

> 快速安装和验证 cert-manager

**作者**: cunzili
**版本**: v1.0
**更新日期**: 2025-01-15

---

## 📋 目录

- [1. 前置条件](#1-前置条件)
- [2. 使用 YAML 安装](#2-使用-yaml-安装)
- [3. 使用 Helm 安装](#3-使用-helm-安装)
- [4. 验证安装](#4-验证安装)
- [5. 卸载 cert-manager](#5-卸载-cert-manager)

---

## 1. 前置条件

### 1.1 检查 Kubernetes 集群

```bash
# 检查集群连接
kubectl cluster-info

# 预期输出:
# Kubernetes control plane is running at ...

# 检查版本
kubectl version --short

# 要求: Kubernetes >= 1.19
```

### 1.2 检查资源

```bash
# 检查节点资源
kubectl top nodes

# 建议资源:
# - CPU: >= 2 核
# - 内存: >= 4GB
# - 磁盘: >= 20GB
```

---

## 2. 使用 YAML 安装

### 2.1 安装步骤

```bash
# 1. 安装 cert-manager CRD（自定义资源定义）
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.crds.yaml

# 预期输出:
# customresourcedefinition.apiextensions.k8s.io/certificaterequests.cert-manager.io created
# customresourcedefinition.apiextensions.k8s.io/certificates.cert-manager.io created
# customresourcedefinition.apiextensions.k8s.io/challenges.acme.cert-manager.io created
# ...

# 2. 添加 cert-manager Jetstack Helm 仓库（可选）
helm repo add jetstack https://charts.jetstack.io
helm repo update

# 3. 安装 cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# 预期输出:
# namespace/cert-manager created
# serviceaccount/cert-manager created
# deployment.apps/cert-manager created
# deployment.apps/cert-manager-cainjector created
# deployment.apps/cert-manager-webhook created
# ...
```

### 2.2 验证安装

```bash
# 1. 检查 Pod 状态
kubectl get pods -n cert-manager

# 预期输出:
# NAME                                      READY   STATUS    RESTARTS   AGE
# cert-manager-xxx                          1/1     Running   0          2m
# cert-manager-cainjector-xxx               1/1     Running   0          2m
# cert-manager-webhook-xxx                  1/1     Running   0          2m

# 2. 检查自定义资源
kubectl get crd | grep cert-manager

# 预期输出:
# certificaterequests.cert-manager.io                  2025-01-15T10:00:00Z
# certificates.cert-manager.io                         2025-01-15T10:00:00Z
# challenges.acme.cert-manager.io                      2025-01-15T10:00:00Z
# clusterissuers.cert-manager.io                       2025-01-15T10:00:00Z
# issuers.cert-manager.io                              2025-01-15T10:00:00Z
# orders.acme.cert-manager.io                          2025-01-15T10:00:00Z

# 3. 检查版本
kubectl get deployment -n cert-manager cert-manager -o jsonpath='{.spec.template.spec.containers[0].image}'

# 预期输出:
# quay.io/jetstack/cert-manager-controller:v1.13.0
```

---

## 3. 使用 Helm 安装

### 3.1 安装 Helm

```bash
# macOS
brew install helm

# Linux
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

# 验证
helm version
```

### 3.2 添加 Helm 仓库

```bash
# 添加 Jetstack Helm 仓库
helm repo add jetstack https://charts.jetstack.io

# 更新仓库
helm repo update

# 搜索 cert-manager
helm search repo jetstack/cert-manager

# 预期输出:
# NAME                    CHART VERSION   APP VERSION     DESCRIPTION
# jetstack/cert-manager   v1.13.0         v1.13.0         A Helm chart for cert-manager
```

### 3.3 安装 cert-manager

```bash
# 创建 namespace
kubectl create namespace cert-manager

# 安装 cert-manager
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --version v1.13.0 \
  --set installCRDs=true

# 预期输出:
# NAME: cert-manager
# LAST DEPLOYED: Wed Jan 15 10:00:00 2025
# NAMESPACE: cert-manager
# STATUS: deployed
# REVISION: 1
```

### 3.4 自定义配置

```bash
# 使用自定义 values.yaml 安装
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --version v1.13.0 \
  --set installCRDs=true \
  --set image.repository=quay.io/jetstack/cert-manager-controller \
  --set image.tag=v1.13.0 \
  --set replicaCount=2 \
  --set resources.requests.cpu=100m \
  --set resources.requests.memory=128Mi
```

---

## 4. 验证安装

### 4.1 快速验证

```bash
# 创建测试 Issuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
EOF

# 创建测试 Certificate
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-cert
  namespace: default
spec:
  secretName: test-cert-tls
  issuerRef:
    name: selfsigned-issuer
    kind: ClusterIssuer
  dnsNames:
  - test.example.com
EOF

# 检查证书状态
kubectl get certificate test-cert

# 预期输出:
# NAME        READY   SECRET          AGE
# test-cert   True    test-cert-tls   1m

# 清理
kubectl delete certificate test-cert
kubectl delete clusterissuer selfsigned-issuer
```

### 4.2 查看 Webhook 配置

```bash
# 检查 ValidatingWebhookConfiguration
kubectl get validatingwebhookconfiguration | grep cert-manager

# 预期输出:
# NAME                                     WEBHOOKS   AGE
# cert-manager-webhook                     1          5m

# 检查 MutatingWebhookConfiguration
kubectl get mutatingwebhookconfiguration | grep cert-manager

# 预期输出:
# NAME                                   WEBHOOKS   AGE
# cert-manager-webhook                    1          5m
```

---

## 5. 卸载 cert-manager

### 5.1 使用 Helm 卸载

```bash
# 卸载 cert-manager
helm uninstall cert-manager -n cert-manager

# 删除 namespace
kubectl delete namespace cert-manager

# 删除 CRD（可选）
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.crds.yaml
```

### 5.2 使用 YAML 卸载

```bash
# 删除 cert-manager
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# 删除 CRD（可选）
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.crds.yaml
```

---

## 常见问题

### Q1: Pod 一直 ImagePullBackOff

**原因**: 镜像拉取失败

**解决**:
```bash
# 检查镜像
kubectl get pod -n cert-manager cert-manager-xxx -o jsonpath='{.spec.containers[0].image}'

# 如果在国内，使用镜像代理
# 方法 1: 修改 deployment
kubectl patch deployment cert-manager -n cert-manager -p '{"spec":{"template":{"spec":{"containers":[{"name":"cert-manager","image":"registry.aliyuncs.com/google_containers/cert-manager-controller:v1.13.0"}]}}}}'

# 方法 2: 使用国内的 YAML
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
# 然后手动修改镜像地址
```

### Q2: Webhook 超时

**症状**: 创建 Issuer 时超时

**解决**:
```bash
# 检查 webhook Service
kubectl get svc -n cert-manager

# 检查网络连通性
kubectl run test --rm -it --restart=Never --image=busybox -- sh -c "nc -zv cert-manager-webhook.cert-manager.svc 443"

# 检查防火墙规则
```

### Q3: CRD 未安装

**症状**: `error: the server doesn't have a resource type "certificates"`

**解决**:
```bash
# 安装 CRD
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.crds.yaml

# 等待 CRD 就绪
kubectl wait --for=condition=established crd/certificates.cert-manager.io
```

---

## 总结

### 安装检查清单

```bash
✅ Kubernetes >= 1.19
✅ 节点资源充足（CPU 2核，内存 4GB）
✅ 安装 CRD
✅ 安装 cert-manager
✅ 验证 Pod 运行
✅ 验证 CRD 创建成功
✅ 测试自签名证书
```

### 下一步

安装完成后，继续阅读:
- [04-configuration.md](./04-configuration.md) - 配置实战
- [05-troubleshooting.md](./05-troubleshooting.md) - 故障排查

---

**下一步**: [04-configuration.md](./04-configuration.md) 🚀
