#!/bin/bash

# Devbox 压测监控脚本
# 用于收集 etcd、containerd、系统资源等指标

set -e

# 配置参数
MONITOR_DURATION=${1:-300}  # 监控持续时间（秒），默认5分钟
INTERVAL=${2:-10}           # 监控间隔（秒），默认10秒
OUTPUT_DIR="./stress_test_results/$(date +%Y%m%d_%H%M%S)"

# 创建输出目录
mkdir -p "$OUTPUT_DIR"

echo "开始 Devbox 压测监控..."
echo "监控持续时间: ${MONITOR_DURATION}秒"
echo "监控间隔: ${INTERVAL}秒"
echo "结果输出目录: $OUTPUT_DIR"

# 监控函数
monitor_etcd() {
    echo "=== ETCD 监控 $(date) ===" >> "$OUTPUT_DIR/etcd.log"
    
    # etcd 存储大小
    kubectl get --raw /metrics | grep etcd_mvcc_db_total_size_in_bytes >> "$OUTPUT_DIR/etcd.log" 2>/dev/null || echo "etcd metrics not available" >> "$OUTPUT_DIR/etcd.log"
    
    # etcd 性能指标
    kubectl get --raw /metrics | grep -E "etcd_(disk|network|server)" >> "$OUTPUT_DIR/etcd.log" 2>/dev/null || true
    
    # API Server 请求延迟
    kubectl get --raw /metrics | grep apiserver_request_duration_seconds >> "$OUTPUT_DIR/apiserver.log" 2>/dev/null || true
    
    echo "" >> "$OUTPUT_DIR/etcd.log"
}

monitor_containerd() {
    echo "=== Containerd 监控 $(date) ===" >> "$OUTPUT_DIR/containerd.log"
    
    # 获取所有节点的 containerd 状态
    for node in $(kubectl get nodes -o jsonpath='{.items[*].metadata.name}'); do
        echo "Node: $node" >> "$OUTPUT_DIR/containerd.log"
        
        # containerd 进程状态
        kubectl debug node/$node -it --image=alpine:latest -- sh -c "
            chroot /host ps aux | grep containerd | head -5
            chroot /host df -h | grep -E '(overlay|devbox)'
        " >> "$OUTPUT_DIR/containerd.log" 2>/dev/null || echo "Failed to get containerd info for $node" >> "$OUTPUT_DIR/containerd.log"
        
        echo "" >> "$OUTPUT_DIR/containerd.log"
    done
}

monitor_system_resources() {
    echo "=== 系统资源监控 $(date) ===" >> "$OUTPUT_DIR/system.log"
    
    # 节点资源使用情况
    kubectl top nodes >> "$OUTPUT_DIR/system.log" 2>/dev/null || echo "kubectl top nodes not available" >> "$OUTPUT_DIR/system.log"
    
    # Pod 资源使用情况
    echo "Pod Resources:" >> "$OUTPUT_DIR/system.log"
    kubectl top pods --all-namespaces | grep devbox >> "$OUTPUT_DIR/system.log" 2>/dev/null || echo "No devbox pods found" >> "$OUTPUT_DIR/system.log"
    
    # 节点详细信息
    echo "Node Details:" >> "$OUTPUT_DIR/system.log"
    kubectl describe nodes | grep -A 5 -E "(Allocated resources|Capacity)" >> "$OUTPUT_DIR/system.log"
    
    echo "" >> "$OUTPUT_DIR/system.log"
}

monitor_devbox_status() {
    echo "=== Devbox 状态监控 $(date) ===" >> "$OUTPUT_DIR/devbox.log"
    
    # Devbox 数量统计
    total_devboxes=$(kubectl get devbox --all-namespaces --no-headers 2>/dev/null | wc -l || echo "0")
    running_devboxes=$(kubectl get devbox --all-namespaces --no-headers 2>/dev/null | grep -c "Running" || echo "0")
    pending_devboxes=$(kubectl get devbox --all-namespaces --no-headers 2>/dev/null | grep -c "Pending" || echo "0")
    failed_devboxes=$(kubectl get devbox --all-namespaces --no-headers 2>/dev/null | grep -c "Failed" || echo "0")
    
    echo "总 Devbox 数量: $total_devboxes" >> "$OUTPUT_DIR/devbox.log"
    echo "运行中: $running_devboxes" >> "$OUTPUT_DIR/devbox.log"
    echo "等待中: $pending_devboxes" >> "$OUTPUT_DIR/devbox.log"
    echo "失败: $failed_devboxes" >> "$OUTPUT_DIR/devbox.log"
    
    # Pod 状态统计
    devbox_pods=$(kubectl get pods --all-namespaces -l "app.kubernetes.io/part-of=devbox" --no-headers 2>/dev/null | wc -l || echo "0")
    running_pods=$(kubectl get pods --all-namespaces -l "app.kubernetes.io/part-of=devbox" --no-headers 2>/dev/null | grep -c "Running" || echo "0")
    pending_pods=$(kubectl get pods --all-namespaces -l "app.kubernetes.io/part-of=devbox" --no-headers 2>/dev/null | grep -c "Pending" || echo "0")
    
    echo "Devbox Pod 总数: $devbox_pods" >> "$OUTPUT_DIR/devbox.log"
    echo "运行中 Pod: $running_pods" >> "$OUTPUT_DIR/devbox.log"
    echo "等待中 Pod: $pending_pods" >> "$OUTPUT_DIR/devbox.log"
    
    echo "" >> "$OUTPUT_DIR/devbox.log"
}

monitor_storage() {
    echo "=== 存储监控 $(date) ===" >> "$OUTPUT_DIR/storage.log"
    
    # LVM 状态
    for node in $(kubectl get nodes -o jsonpath='{.items[*].metadata.name}'); do
        echo "Node: $node LVM Status:" >> "$OUTPUT_DIR/storage.log"
        
        kubectl debug node/$node -it --image=alpine:latest -- sh -c "
            chroot /host which vgs && chroot /host vgs
            chroot /host which lvs && chroot /host lvs | grep devbox
            chroot /host df -h | grep -E '(devbox|lvm)'
        " >> "$OUTPUT_DIR/storage.log" 2>/dev/null || echo "Failed to get LVM info for $node" >> "$OUTPUT_DIR/storage.log"
        
        echo "" >> "$OUTPUT_DIR/storage.log"
    done
    
    # PV/PVC 状态
    echo "PV Status:" >> "$OUTPUT_DIR/storage.log"
    kubectl get pv | grep devbox >> "$OUTPUT_DIR/storage.log" 2>/dev/null || echo "No devbox PVs" >> "$OUTPUT_DIR/storage.log"
    
    echo "PVC Status:" >> "$OUTPUT_DIR/storage.log"
    kubectl get pvc --all-namespaces | grep devbox >> "$OUTPUT_DIR/storage.log" 2>/dev/null || echo "No devbox PVCs" >> "$OUTPUT_DIR/storage.log"
    
    echo "" >> "$OUTPUT_DIR/storage.log"
}

monitor_events() {
    echo "=== 事件监控 $(date) ===" >> "$OUTPUT_DIR/events.log"
    
    # 获取最近的 Kubernetes 事件
    kubectl get events --all-namespaces --sort-by='.lastTimestamp' | tail -20 >> "$OUTPUT_DIR/events.log"
    
    # 获取 devbox 相关事件
    echo "Devbox 相关事件:" >> "$OUTPUT_DIR/events.log"
    kubectl get events --all-namespaces --field-selector involvedObject.kind=Devbox >> "$OUTPUT_DIR/events.log" 2>/dev/null || echo "No devbox events" >> "$OUTPUT_DIR/events.log"
    
    echo "" >> "$OUTPUT_DIR/events.log"
}

# 创建汇总报告
generate_summary() {
    echo "生成监控汇总报告..."
    
    cat > "$OUTPUT_DIR/summary.md" << EOF
# Devbox 压测监控报告

## 测试信息
- 开始时间: $(cat "$OUTPUT_DIR/start_time")
- 结束时间: $(date)
- 监控持续时间: ${MONITOR_DURATION}秒
- 监控间隔: ${INTERVAL}秒

## 监控结果概览

### Devbox 状态统计
\`\`\`
$(tail -10 "$OUTPUT_DIR/devbox.log" | grep -E "(总|运行|等待|失败)")
\`\`\`

### 系统资源使用
\`\`\`
$(tail -20 "$OUTPUT_DIR/system.log" | grep -A 10 "kubectl top nodes")
\`\`\`

### 存储状态
\`\`\`
$(tail -20 "$OUTPUT_DIR/storage.log" | grep -E "(devbox|lvm)")
\`\`\`

## 详细日志文件
- etcd.log: etcd 性能指标
- containerd.log: containerd 状态
- system.log: 系统资源使用
- devbox.log: devbox 状态统计
- storage.log: 存储状态
- events.log: Kubernetes 事件

## 分析建议
1. 查看 etcd.log 中的存储大小变化趋势
2. 检查 system.log 中的资源使用是否接近上限
3. 观察 devbox.log 中失败和等待的数量变化
4. 关注 events.log 中的错误事件

EOF

    echo "汇总报告已生成: $OUTPUT_DIR/summary.md"
}

# 主监控循环
main() {
    # 记录开始时间
    echo "$(date)" > "$OUTPUT_DIR/start_time"
    
    echo "开始监控循环..."
    
    end_time=$(($(date +%s) + MONITOR_DURATION))
    
    while [ $(date +%s) -lt $end_time ]; do
        echo "执行监控检查... $(date)"
        
        # 并行执行监控任务
        monitor_etcd &
        monitor_containerd &
        monitor_system_resources &
        monitor_devbox_status &
        monitor_storage &
        monitor_events &
        
        # 等待所有监控任务完成
        wait
        
        echo "监控检查完成，等待 ${INTERVAL} 秒..."
        sleep $INTERVAL
    done
    
    echo "监控完成，生成汇总报告..."
    generate_summary
}

# 信号处理
cleanup() {
    echo "收到中断信号，正在生成报告..."
    generate_summary
    exit 0
}

trap cleanup INT TERM

# 启动监控
main

echo "监控已完成，结果保存在: $OUTPUT_DIR"
