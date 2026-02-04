# Pod 健康检查故障排查指南

> 解决 Liveness/Readiness Probe 失败问题

---

## 问题分析

### 错误信息解读

```
Warning  Unhealthy  Liveness probe failed: Get "http://10.0.0.202:80/":
dial tcp 10.0.0.202:80: connect: connection refused

Warning  Unhealthy  Readiness probe failed: Get "http://10.0.0.202:80/":
dial tcp 10.0.0.202:80: connect: connection refused
```

**关键信息**：
- `connection refused` = 端口 80 没有被监听
- nginx 容器启动了，但 nginx 服务没有正常运行

### 根本原因

**你的镜像不是标准的 nginx 镜像！**

从 Events 中可以看到：
```
Container image "crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com/
linzichun-namespace/nginx-1.22.1:v1.1"
```

这是你自己的镜像仓库中的自定义镜像，可能：
1. nginx 配置有问题
2. nginx 启动命令被修改了
3. 80 端口被修改了
4. 镜像构建有问题

---

## 排查步骤

### 步骤 1: 查看 Pod 日志

```bash
# 查看 Pod 名称
kubectl get pods

# 查看 Pod 日志（最重要！）
kubectl logs <pod-name>

# 示例
kubectl logs myapp-5ff6cc776c-j65zl
```

**期望看到**（正常的 nginx 日志）:
```
/docker-entrypoint.sh: /docker-entrypoint.d/ is not empty, will attempt to perform configuration
/docker-entrypoint.sh: Looking for shell scripts in /docker-entrypoint.d/
/docker-entrypoint.sh: Launching /docker-entrypoint.d/10-listen-on-ipv6-by-default.sh
10-listen-on-ipv6-by-default.sh: Getting the checksum of /etc/nginx/conf.d/default.conf
10-listen-on-ipv6-by-default.sh: Enabled listen on IPv6 address in /etc/nginx/conf.d/default.conf
/docker-entrypoint.sh: Launching /docker-entrypoint.d/20-envsubst-on-templates.sh
/docker-entrypoint.sh: Configuration complete; ready for start up
```

**可能看到**（有问题的日志）:
```
nginx: [emerg] invalid number of arguments in "worker_processes" directive
# 或
nginx: [emerg] invalid host in upstream
# 或
nginx: [alert] could not open error log file: open() "/var/log/nginx/error.log" failed (13: Permission denied)
```

### 步骤 2: 进入容器检查

```bash
# 进入容器
kubectl exec -it <pod-name> -- /bin/sh

# 或
kubectl exec -it <pod-name> -- /bin/bash
```

**在容器内执行**:

```bash
# 1. 检查 nginx 进程
ps aux | grep nginx

# 期望输出（正常）:
# root         1  0.0  0.0  10612  3244 ?        Ss   10:00   0:00 nginx: master process nginx -g daemon off;
# nginx        29  0.0  0.0  11060  3772 ?        S    10:00   0:00 nginx: worker process

# 实际输出（可能）:
# （没有 nginx 进程）

# 2. 检查端口监听
netstat -tlnp

# 期望输出（正常）:
# tcp        0      0 0.0.0.0:80              0.0.0.0:*               LISTEN      1/nginx

# 实际输出（可能）:
# （没有 80 端口监听）

# 3. 手动启动 nginx（如果进程不存在）
nginx

# 4. 查看错误日志
cat /var/log/nginx/error.log

# 5. 检查 nginx 配置
nginx -t

# 6. 查看配置文件
cat /etc/nginx/nginx.conf
cat /etc/nginx/conf.d/default.conf
```

### 步骤 3: 检查镜像

```bash
# 查看使用的镜像
kubectl describe pod <pod-name> | grep Image

# 你的输出:
# Image:  crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com/
#         linzichun-namespace/nginx-1.22.1:v1.1

# 拉取镜像到本地检查
docker pull crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com/linzichun-namespace/nginx-1.22.1:v1.1

# 本地运行测试
docker run -d -p 8080:80 --name test-nginx \
  crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com/linzichun-namespace/nginx-1.22.1:v1.1

# 检查
docker ps
docker logs test-nginx
curl http://localhost:8080

# 清理
docker stop test-nginx
docker rm test-nginx
```

---

## 解决方案

### 方案 1: 使用官方 nginx 镜像（推荐）

```yaml
# deployment-official.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  labels:
    app: myapp
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
        image: nginx:1.25  # ← 使用官方镜像
        ports:
        - containerPort: 80
        readinessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 5
          periodSeconds: 10
        livenessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 15
          periodSeconds: 20
```

```bash
# 删除旧的 Deployment
kubectl delete deployment myapp

# 应用新的配置
kubectl apply -f deployment-official.yaml

# 查看 Pod 状态
kubectl get pods -w
```

### 方案 2: 修复自定义镜像

如果必须使用你的自定义镜像，需要检查 Dockerfile：

**检查 Dockerfile**:

```dockerfile
# 你的 Dockerfile 可能有问题
FROM nginx:1.22.1

# ❌ 可能的问题 1: 修改了配置但语法错误
COPY nginx.conf /etc/nginx/nginx.conf

# ❌ 可能的问题 2: 启动命令被覆盖
CMD ["custom-start-script"]

# ❌ 可能的问题 3: 修改了监听端口
EXPOSE 8080
```

**推荐的 Dockerfile**:

```dockerfile
FROM nginx:1.22.1

# 只添加自定义配置，不要覆盖主配置
COPY default.conf /etc/nginx/conf.d/default.conf

# 不要修改 CMD
# CMD ["nginx", "-g", "daemon off;"]  # 已存在，不需要

# 确保监听 80 端口
EXPOSE 80
```

**自定义配置示例**:

```nginx
# default.conf
server {
    listen 80;
    server_name localhost;

    location / {
        root /usr/share/nginx/html;
        index index.html index.htm;
    }

    # 健康检查端点
    location /health {
        access_log off;
        return 200 "healthy\n";
        add_header Content-Type text/plain;
    }
}
```

**重新构建镜像**:

```bash
# 构建镜像
docker build -t nginx-custom:latest .

# 打标签
docker tag nginx-custom:latest \
  crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com/linzichun-namespace/nginx-1.22.1:v1.2

# 推送到镜像仓库
docker push crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com/linzichun-namespace/nginx-1.22.1:v1.2

# 更新 Deployment 使用新镜像
kubectl set image deployment/myapp myapp=crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com/linzichun-namespace/nginx-1.22.1:v1.2

# 查看更新状态
kubectl rollout status deployment/myapp
```

### 方案 3: 调整健康检查（临时方案）

如果镜像确实有问题，可以先禁用健康检查：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
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
        image: nginx:latest  # 使用你的镜像
        ports:
        - containerPort: 80
        # 暂时注释掉健康检查
        # readinessProbe:
        #   httpGet:
        #     path: /
        #     port: 80
        # livenessProbe:
        #   httpGet:
        #     path: /
        #     port: 80
```

---

## 快速验证脚本

```bash
#!/bin/bash

echo "=== Pod 健康检查快速诊断 ==="

# 1. 获取 Pod 名称
POD_NAME=$(kubectl get pods -l app=myapp -o jsonpath='{.items[0].metadata.name}')
echo "Pod 名称: $POD_NAME"

# 2. 查看 Pod 状态
echo -e "\n=== Pod 状态 ==="
kubectl get pod $POD_NAME

# 3. 查看 Pod 详情
echo -e "\n=== Pod 详情 ==="
kubectl describe pod $POD_NAME | tail -20

# 4. 查看日志
echo -e "\n=== Pod 日志（最后 50 行）==="
kubectl logs $POD_NAME --tail=50

# 5. 检查端口
echo -e "\n=== 端口监听检查 ==="
kubectl exec $POD_NAME -- netstat -tlnp 2>/dev/null || \
  kubectl exec $POD_NAME -- /bin/sh -c "netstat -tlnp" 2>/dev/null || \
  echo "netstat 不可用，尝试其他方法..."

# 6. 检查进程
echo -e "\n=== 进程检查 ==="
kubectl exec $POD_NAME -- ps aux | grep nginx

# 7. 测试内部访问
echo -e "\n=== 内部访问测试 ==="
kubectl exec $POD_NAME -- wget -O- -q http://localhost:80/ || \
  kubectl exec $POD_NAME -- curl -s http://localhost:80/ || \
  echo "无法访问 localhost:80"

# 8. 查看镜像
echo -e "\n=== 使用的镜像 ==="
kubectl get pod $POD_NAME -o jsonpath='{.spec.containers[0].image}'

echo -e "\n=== 诊断完成 ==="
```

保存为 `check-pod.sh`，然后：
```bash
chmod +x check-pod.sh
./check-pod.sh
```

---

## 常见问题清单

### 问题 1: 镜像拉取失败

```
Failed to pull image "xxx": rpc error: code = Unknown desc =
Error response from daemon: pull access denied
```

**解决**:
```bash
# 登录镜像仓库
docker login crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com

# 创建 Secret
kubectl create secret docker-registry regcred \
  --docker-server=crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com \
  --docker-username=<your-username> \
  --docker-password=<your-password>

# 在 Deployment 中引用
spec:
  template:
    spec:
      imagePullSecrets:
      - name: regcred
```

### 问题 2: CrashLoopBackOff

```
State:       Running
  Started:    Wed, 15 Jan 2025 10:00:00 +0800
  Last State:     Terminated
    Reason:       Error
    Exit Code:    1
```

**排查**:
```bash
# 查看日志
kubectl logs <pod-name>

# 查看上一个容器的日志（如果重启过）
kubectl logs <pod-name> --previous
```

### 问题 3: ImagePullBackOff

```
Status:       ImagePullBackOff
```

**原因**: 镜像不存在或无法访问

**解决**:
```bash
# 验证镜像是否存在
docker pull <image-name>

# 或使用 docker.io 官方镜像
image: nginx:1.25
```

### 问题 4: 端口不匹配

```
Liveness probe failed: dial tcp 10.0.0.202:8080: i/o timeout
```

**检查**:
```yaml
# 确认容器内的端口
ports:
- containerPort: 8080  # ← 必须与容器实际监听的端口一致

# 调整健康检查
livenessProbe:
  httpGet:
    port: 8080  # ← 与 containerPort 一致
```

---

## 完整的示例配置

### 使用官方镜像的最佳实践

```yaml
# deployment-complete.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  labels:
    app: myapp
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
        image: nginx:1.25-alpine  # 使用 alpine 版本（更小）
        ports:
        - name: http
          containerPort: 80
          protocol: TCP

        # 健康检查
        livenessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 30   # 给 nginx 足够的启动时间
          periodSeconds: 10
          timeoutSeconds: 5
          successThreshold: 1
          failureThreshold: 3

        readinessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 5
          periodSeconds: 5
          timeoutSeconds: 3
          successThreshold: 1
          failureThreshold: 3

        # 资源限制
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 200m
            memory: 256Mi

        # 环境变量
        env:
        - name: TZ
          value: "Asia/Shanghai"
```

---

## 调试技巧

### 技巧 1: 使用临时 Pod 调试

```bash
# 运行一个临时 Pod，可以进入目标 Pod 的网络命名空间
kubectl run debug --rm -it --image=nicolaka/netshoot --restart=Never -- \
  /bin/sh

# 在临时 Pod 中测试
wget -O- http://<pod-ip>:80/
```

### 技巧 2: 使用 kubectl port-forward

```bash
# 转发 Pod 端口到本地
kubectl port-forward <pod-name> 8080:80

# 在另一个终端测试
curl http://localhost:8080/
```

### 技巧 3: 查看事件流

```bash
# 持续监控事件
kubectl get events --watch

# 过滤特定 Pod 的事件
kubectl get events --field-selector involvedObject.name=<pod-name>
```

---

## 总结

### 问题根源

你的自定义镜像 `crpi-5yq5lj8mtkm1w8pm.cn-hangzhou.personal.cr.aliyuncs.com/linzichun-namespace/nginx-1.22.1:v1.1` 中的 nginx 没有正常启动监听 80 端口。

### 立即解决

**最简单的方法**：
```bash
# 1. 删除有问题的 Deployment
kubectl delete deployment myapp

# 2. 使用官方镜像重新创建
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
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
EOF

# 3. 等待 Pod 就绪
kubectl get pods -w
```

### 后续步骤

Pod 正常运行后，继续配置 Service 和 Ingress：

```bash
# 4. 创建 Service
kubectl expose deployment myapp --port=80 --target-port=80

# 5. 创建 Ingress
cat <<EOF | kubectl apply -f -
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  rules:
  - host: myapp.local  # 或使用你的域名
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp
            port:
              number: 80
EOF

# 6. 查看资源
kubectl get all
kubectl get ingress
```

---

**作者**: cunzili
**日期**: 2025-01-15
**版本**: v1.0
