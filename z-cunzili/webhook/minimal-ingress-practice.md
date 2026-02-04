# Ingress 最小实践案例 - 从零到可访问

> 一个完整的、可验证的 Ingress 实践指南

---

## 📋 前置准备

### 1. 检查环境

```bash
# 检查 Kubernetes 集群是否正常
kubectl cluster-info

# 期望输出:
# Kubernetes control plane is running at ...
# CoreDNS is running at ...

# 检查节点状态
kubectl get nodes

# 期望输出:
# NAME     STATUS   ROLES           AGE   VERSION
# master   Ready    control-plane   10d   v1.xx.x
# worker   Ready    <none>          10d   v1.xx.x
```

### 2. 检查是否有 Ingress Controller

```bash
# 检查 Ingress Controller
kubectl get pods -n ingress-nginx 2>/dev/null || \
kubectl get pods -n kube-system | grep ingress

# 如果没有 Ingress Controller，需要先安装
# 下面会提供安装步骤
```

---

## 🚀 完整实践步骤

### 步骤 1: 部署应用（Deployment）

#### 1.1 创建 Deployment 配置文件

```bash
# 创建配置文件
cat > deployment.yaml <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  labels:
    app: myapp
spec:
  replicas: 2
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
        # 使用阿里云镜像（国内可访问）
        image: registry.cn-hangzhou.aliyuncs.com/google_containers/nginx:1.25
        ports:
        - containerPort: 80
          name: http
        # 资源限制（防止占用过多资源）
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 200m
            memory: 256Mi
        # 健康检查
        livenessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 30   # 给足够的启动时间
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 3
EOF
```

#### 1.2 应用配置

```bash
# 部署应用
kubectl apply -f deployment.yaml

# 预期输出:
# deployment.apps/myapp created
```

#### 1.3 验证 Deployment

```bash
# 查看 Deployment 状态
kubectl get deployment myapp

# 预期输出:
# NAME    READY   UP-TO-DATE   AVAILABLE   AGE
# myapp   2/2     2            2           30s

# 等待 Pod 就绪（可能需要 1-2 分钟）
kubectl get pods -w

# 按 Ctrl+C 停止监控

# 查看 Pod 详情
kubectl get pods -l app=myapp

# 预期输出:
# NAME                     READY   STATUS    RESTARTS   AGE
# myapp-xxx-xxx            1/1     Running   0          1m
# myapp-yyy-yyy            1/1     Running   0          1m

# 检查 READY 是否为 1/1（重要！）
# 1/1 = 健康检查通过
# 0/1 = 健康检查失败
```

#### 1.4 进入 Pod 验证

```bash
# 获取 Pod 名称
POD_NAME=$(kubectl get pods -l app=myapp -o jsonpath='{.items[0].metadata.name}')
echo "Pod 名称: $POD_NAME"

# 进入 Pod 测试
kubectl exec -it $POD_NAME -- /bin/sh

# 在 Pod 内执行以下命令:

# 1. 检查 nginx 进程
ps aux | grep nginx

# 预期输出:
# root         1  0.0  0.0  10612  3244 ?        Ss   10:00   0:00 nginx: master process nginx -g daemon off;
# nginx        29  0.0  0.0  11060  3772 ?        S    10:00   0:00 nginx: worker process

# 2. 测试本地访问
wget -O- -q http://localhost/

# 预期输出: HTML 内容（欢迎页面）

# 3. 查看监听端口
netstat -tlnp

# 预期输出:
# tcp        0      0 0.0.0.0:80              0.0.0.0:*               LISTEN      1/nginx

# 4. 退出 Pod
exit
```

---

### 步骤 2: 创建 Service

#### 2.1 创建 Service 配置文件

```bash
cat > service.yaml <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  selector:
    app: myapp
  ports:
  - name: http
    port: 80          # Service 端口（集群内访问的端口）
    targetPort: http  # Pod 端口（containerPort 定义的端口名）
    protocol: TCP
  type: ClusterIP     # 集群内部访问类型
EOF
```

#### 2.2 应用配置

```bash
# 创建 Service
kubectl apply -f service.yaml

# 预期输出:
# service/myapp-service created
```

#### 2.3 验证 Service

```bash
# 查看 Service
kubectl get svc myapp-service

# 预期输出:
# NAME            TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)   AGE
# myapp-service   ClusterIP   10.96.100.50    <none>        80/TCP    10s
#                                  ↑
#                           虚拟 IP（自动分配）

# 查看 Service 详情
kubectl describe svc myapp-service

# 预期输出包含:
# Selector:               app=myapp
# Endpoints:              10.244.1.5:80,10.244.1.6:80
#                          ↑
#                   Pod IP 列表（自动发现）

# 查看 Endpoints
kubectl get endpoints myapp-service

# 预期输出:
# NAME            ENDPOINTS                          AGE
# myapp-service   10.244.1.5:80,10.244.1.6:80        1m
#                          ↑
#                  必须有 IP 地址！如果为空，说明 Pod 没有被选中
```

#### 2.4 测试 Service 连通性

```bash
# 方法 1: 使用临时 Pod 测试
kubectl run test-pod --image=busybox:1.28 --rm -it --restart=Never -- \
  wget -O- -q http://myapp-service/

# 预期输出: HTML 内容

# 方法 2: 使用 port-forward（本地测试）
kubectl port-forward svc/myapp-service 8080:80 &

# 在另一个终端测试
curl http://localhost:8080/

# 预期输出: HTML 内容

# 清理 port-forward
killall kubectl
```

---

### 步骤 3: 安装 Ingress Controller（如果没有）

#### 3.1 检查是否已安装

```bash
# 检查是否有 Ingress Controller
kubectl get pods -n ingress-nginx 2>/dev/null

# 如果有 Pod 运行，说明已安装，可以跳过此步骤

# 如果没有，继续下面的安装步骤
```

#### 3.2 安装 Ingress Controller（选择一种方式）

**方式 A: 使用 kubectl apply（推荐）**

```bash
# 安装 NGINX Ingress Controller
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml

# 等待 Pod 就绪（需要 2-3 分钟）
kubectl get pods -n ingress-nginx -w

# 预期输出（最终状态）:
# NAME                                       READY   STATUS      RESTARTS   AGE
# ingress-nginx-controller-xxx               1/1     Running     0          2m
```

**方式 B: 使用 Helm（需要先安装 Helm）**

```bash
# 添加 Helm 仓库
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx

# 安装
helm install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --set controller.image.repository=registry.cn-hangzhou.aliyuncs.com/google_containers/nginx-ingress-controller \
  --set controller.image.tag=v1.9.4 \
  --set controller.image.digest="" \
  --set controller.admissionWebhooks.enabled=false

# 查看状态
kubectl get pods -n ingress-nginx
```

#### 3.3 验证 Ingress Controller

```bash
# 查看 Ingress Controller Pod
kubectl get pods -n ingress-nginx

# 预期输出:
# NAME                                       READY   STATUS    RESTARTS   AGE
# ingress-nginx-controller-xxx               1/1     Running   0          5m

# 查看 Ingress Controller Service
kubectl get svc -n ingress-nginx

# 预期输出:
# NAME                       TYPE           CLUSTER-IP      EXTERNAL-IP      PORT(S)
# ingress-nginx-controller   LoadBalancer   10.96.100.100   <pending>        80:xxxxx/TCP,443:xxxxx/TCP
#                                                       ↑
#                                           如果是云环境，会有公网 IP
#                                           本地环境显示 <pending> 是正常的

# 查看日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx --tail=20
```

---

### 步骤 4: 创建 Ingress

#### 4.1 创建 Ingress 配置文件

```bash
cat > ingress.yaml <<'EOF'
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    # 使用注解配置 Nginx
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  ingressClassName: nginx  # 指定使用 nginx Ingress Controller
  rules:
  - host: myapp.local       # 使用本地测试域名
    http:
      paths:
      - path: /             # 匹配根路径
        pathType: Prefix    # 前缀匹配
        backend:
          service:
            name: myapp-service  # 转发到这个 Service
            port:
              number: 80
EOF
```

**配置说明**:

| 字段 | 值 | 说明 |
|------|-----|------|
| `host` | `myapp.local` | 访问的域名（需要能解析到 Ingress Controller）|
| `path` | `/` | 匹配的路径 |
| `pathType` | `Prefix` | 前缀匹配（`/api` 会匹配 `/api/*`）|
| `service.name` | `myapp-service` | 转发到的 Service 名称 |
| `service.port.number` | `80` | Service 的端口 |

#### 4.2 应用配置

```bash
# 创建 Ingress
kubectl apply -f ingress.yaml

# 预期输出:
# ingress.networking.k8s.io/myapp-ingress created
```

#### 4.3 验证 Ingress

```bash
# 查看 Ingress
kubectl get ingress myapp-ingress

# 预期输出:
# NAME            CLASS   HOSTS         ADDRESS        PORTS   AGE
# myapp-ingress   nginx   myapp.local   192.168.1.10   80      1m
#                                           ↑
#                             Ingress Controller 所在节点的 IP

# 查看 Ingress 详情
kubectl describe ingress myapp-ingress

# 预期输出包含:
# Rules:
#   Host          Path  Backends
#   ----          ----  --------
#   myapp.local
#                 /   myapp-service:80 (<IP>:<Port>)
```

---

### 步骤 5: 配置 DNS 并测试访问

#### 5.1 获取 Ingress Controller 的访问地址

```bash
# 方法 1: 如果是 LoadBalancer 类型（云环境）
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 如果 EXTERNAL-IP 有值，使用该 IP
# ADDRESS        PORTS
# 1.2.3.4        80:xxxxx/TCP
# ↑ 使用这个 IP

# 方法 2: 如果是 NodePort 或本地环境
kubectl get pods -n ingress-nginx -o wide

# 使用节点的 IP
# READY   STATUS    RESTARTS   AGE   IP              NODE
# 1/1     Running   0          5m    10.244.1.2      master   ← 使用这个 NODE 的 IP

# 方法 3: 使用 hostNetwork（如果配置了）
kubectl get nodes -o wide

# 使用任意节点的 IP
```

#### 5.2 配置本地 DNS

**方案 A: 修改 /etc/hosts（最简单）**

```bash
# 获取 Ingress Controller 的 IP
INGRESS_IP=$(kubectl get pods -n ingress-nginx -o wide | grep controller | awk '{print $6}' | head -1)
echo "Ingress Controller IP: $INGRESS_IP"

# 修改 hosts 文件（需要 sudo）
echo "$INGRESS_IP myapp.local" | sudo tee -a /etc/hosts

# 验证
grep myapp.local /etc/hosts

# 测试 DNS 解析
ping -c 1 myapp.local
```

**方案 B: 使用 CoreDNS（如果在集群内测试）**

```bash
# 使用临时 Pod 测试
kubectl run test-dns --image=busybox:1.28 --rm -it --restart=Never -- \
  nslookup myapp.local
```

#### 5.3 测试访问

```bash
# 测试 1: 使用域名访问
curl http://myapp.local/

# 预期输出: HTML 内容（nginx 欢迎页面）

# 测试 2: 使用 IP + Host 头访问
curl -H "Host: myapp.local" http://$INGRESS_IP/

# 预期输出: HTML 内容

# 测试 3: 在浏览器中访问
# 打开浏览器，访问: http://myapp.local/
```

#### 5.4 查看日志

```bash
# 查看 Ingress Controller 日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx --tail=20 -f

# 访问 http://myapp.local/，观察日志输出

# 预期看到类似的日志:
# <IP> - - [<时间>] "GET / HTTP/1.1" 200 615 "-" "curl/7.68.0"  "-"
```

---

## 🔍 故障排查

### 问题 1: Pod 无法启动（CrashLoopBackOff）

```bash
# 查看 Pod 状态
kubectl get pods

# 查看 Pod 详情
kubectl describe pod <pod-name>

# 查看日志
kubectl logs <pod-name>

# 常见原因:
# - 镜像拉取失败
# - 健康检查失败
# - 资源不足
```

### 问题 2: Service 无法访问

```bash
# 检查 Endpoints
kubectl get endpoints myapp-service

# 如果为空:
# - 检查 Service 的 selector 是否与 Pod 的 labels 匹配
kubectl get pods --show-labels
kubectl describe svc myapp-service

# 测试 Pod 直接访问
POD_IP=$(kubectl get pods -l app=myapp -o jsonpath='{.items[0].status.podIP}')
kubectl run test --image=busybox:1.28 --rm -it --restart=Never -- \
  wget -O- -q http://$POD_IP/
```

### 问题 3: Ingress 无法访问

```bash
# 1. 检查 Ingress 是否创建
kubectl get ingress

# 2. 检查 ADDRESS 字段
kubectl get ingress myapp-ingress

# 3. 检查 Ingress Controller 日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx

# 4. 测试 Ingress Controller
curl -H "Host: myapp.local" http://<ingress-controller-ip>/

# 5. 检查防火墙规则
sudo iptables -L -n | grep 80
sudo ufw status
```

### 问题 4: DNS 解析失败

```bash
# 检查 /etc/hosts
cat /etc/hosts | grep myapp.local

# 测试 IP 连通性
ping <ingress-controller-ip>

# 使用 IP 直接访问
curl http://<ingress-controller-ip>/ -H "Host: myapp.local"
```

---

## 📊 完整验证流程

```bash
#!/bin/bash

echo "=== 完整验证流程 ==="

# 1. 检查 Pod
echo -e "\n1. 检查 Pod 状态"
kubectl get pods -l app=myapp
if [ $? -eq 0 ]; then
    echo "✓ Pod 运行正常"
else
    echo "✗ Pod 有问题"
    exit 1
fi

# 2. 检查 Service
echo -e "\n2. 检查 Service"
kubectl get svc myapp-service
ENDPOINTS=$(kubectl get endpoints myapp-service -o jsonpath='{.subsets[0].addresses[*].ip}')
if [ -n "$ENDPOINTS" ]; then
    echo "✓ Service Endpoints: $ENDPOINTS"
else
    echo "✗ Service Endpoints 为空"
    exit 1
fi

# 3. 检查 Ingress
echo -e "\n3. 检查 Ingress"
kubectl get ingress myapp-ingress
ADDRESS=$(kubectl get ingress myapp-ingress -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
if [ -n "$ADDRESS" ]; then
    echo "✓ Ingress Address: $ADDRESS"
else
    echo "! Ingress Address 为空（可能正常）"
    ADDRESS=$(kubectl get pods -n ingress-nginx -o wide | grep controller | awk '{print $6}' | head -1)
    echo "  使用 Pod 所在节点 IP: $ADDRESS"
fi

# 4. 测试访问
echo -e "\n4. 测试访问"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: myapp.local" http://$ADDRESS/)
if [ "$HTTP_CODE" = "200" ]; then
    echo "✓ 访问成功 (HTTP $HTTP_CODE)"
else
    echo "✗ 访问失败 (HTTP $HTTP_CODE)"
    exit 1
fi

# 5. 显示欢迎页面
echo -e "\n5. 显示页面内容"
curl -s -H "Host: myapp.local" http://$ADDRESS/ | head -20

echo -e "\n=== 所有检查通过 ==="
```

保存为 `verify.sh`，然后执行：

```bash
chmod +x verify.sh
./verify.sh
```

---

## 📝 清理环境

```bash
# 删除 Ingress
kubectl delete ingress myapp-ingress

# 删除 Service
kubectl delete service myapp-service

# 删除 Deployment
kubectl delete deployment myapp

# 删除测试 Pod
kubectl delete pod test-pod 2>/dev/null

# 删除 /etc/hosts 配置
sudo sed -i '/myapp.local/d' /etc/hosts

# 验证清理
kubectl get all
```

---

## 🎯 总结

### 架构流程

```
用户访问: http://myapp.local
    ↓
DNS 解析: /etc/hosts → Ingress Controller IP
    ↓
Ingress Controller: 匹配 host=myapp.local
    ↓
Ingress 规则: path=/ → myapp-service
    ↓
Service: myapp-service (ClusterIP)
    ↓
Endpoints: Pod IP 列表（负载均衡）
    ↓
Pod: nginx 容器
    ↓
返回: HTML 页面
```

### 关键点

1. **Pod**: 运行实际应用，需要有健康的容器
2. **Service**: 提供稳定的访问入口，自动发现 Pod
3. **Ingress**: 根据 Host/Path 路由到不同的 Service
4. **Ingress Controller**: 实际处理 HTTP 请求的组件
5. **DNS**: 将域名解析到 Ingress Controller 的 IP

### 常用命令

```bash
# 查看所有资源
kubectl get all

# 查看特定标签的 Pod
kubectl get pods -l app=myapp

# 查看 Pod 日志
kubectl logs <pod-name>

# 进入 Pod
kubectl exec -it <pod-name> -- /bin/sh

# 端口转发
kubectl port-forward svc/myapp-service 8080:80

# 查看事件
kubectl get events --sort-by='.lastTimestamp'

# 查看资源详情
kubectl describe <resource-type> <resource-name>
```

---

**作者**: cunzili
**日期**: 2025-01-15
**版本**: v1.0
