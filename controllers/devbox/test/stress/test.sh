#!/bin/bash

# Devbox 压测脚本
# 用于测试 devbox 的创建、执行命令、提交和删除功能

# 配置参数
devboxCount=10
devboxNamePrefix="test-devbox"
execCommand="dd if=/dev/random of=file.bin bs=1M count=100"
concurrency=10

# 创建指定的单个 devbox
CreateDevbox() {
    local index=$1
    if [ -z "$index" ]; then
        echo "Usage: CreateDevbox <devbox_index>"
        return 1
    fi
    
    local name="${devboxNamePrefix}-${index}"
    echo "Creating devbox: $name"
    
    # 使用 here-doc 创建 devbox，确保 YAML 使用空格缩进
    cat <<-YAML | kubectl apply -f -
	apiVersion: devbox.sealos.io/v1alpha2
	kind: Devbox
	metadata:
	  name: ${name}
	  namespace: default
	spec:
	  network:
	    type: NodePort
	  resource:
	    cpu: 2000m
	    memory: 4096Mi
	  image: ghcr.io/labring-actions/devbox/go-1.23.0:13aacd8
	  runtimeClassName: devbox-runtime
	  storageLimit: 10Gi
	  config:
	    workingDir: /home/devbox/project
	YAML
    
    # 等待 devbox 运行
    kubectl wait --for=jsonpath='{.status.phase}'=Running devbox ${name} --timeout=300s
    if [ $? -eq 0 ]; then
        echo "Devbox $name is running"
    else
        echo "Failed to create devbox $name"
        return 1
    fi
}

# 对指定 devbox 执行自定义命令
ExecCommand() {
    local index=$1
    local command=$2
    
    if [ -z "$index" ] || [ -z "$command" ]; then
        echo "Usage: ExecCommand <devbox_index> <command>"
        return 1
    fi
    
    local devboxName="${devboxNamePrefix}-${index}"
    echo "Executing command on devbox $devboxName: $command"
    
    kubectl exec ${devboxName} -n default -- sh -c "${command}"
    if [ $? -eq 0 ]; then
        echo "Command executed successfully on $devboxName"
    else
        echo "Failed to execute command on $devboxName"
        return 1
    fi
}

# 并发控制：限制后台任务数量不超过 concurrency
wait_for_available_slot() {
    while :; do
        local running
        running=$(jobs -rp | wc -l | tr -d ' ')
        if [ "$running" -lt "$concurrency" ]; then
            break
        fi
        sleep 0.2
    done
}

# 检查 devbox 状态
CheckDevboxStatus() {
    local index=$1
    
    if [ -z "$index" ]; then
        echo "Usage: CheckDevboxStatus <devbox_index>"
        return 1
    fi
    
    local devboxName="${devboxNamePrefix}-${index}"
    echo "Checking status of devbox: $devboxName"
    
    kubectl get devbox ${devboxName} -n default -o wide
}

# 提交 devbox（停止 -> 运行）
Commit() {
    local index=$1
    
    if [ -z "$index" ]; then
        echo "Usage: Commit <devbox_index>"
        return 1
    fi
    
    local devboxName="${devboxNamePrefix}-${index}"
    echo "Committing devbox: $devboxName"
    
    # 停止 devbox
    kubectl patch devbox ${devboxName} -n default --type=merge -p '{"spec":{"state":"Stopped"}}'
    kubectl wait --for=jsonpath='{.status.state}'=Stopped devbox ${devboxName} --timeout=60s
    
    if [ $? -ne 0 ]; then
        echo "Failed to stop devbox $devboxName"
        return 1
    fi
    
    # 重新启动 devbox
    kubectl patch devbox ${devboxName} -n default --type=merge -p '{"spec":{"state":"Running"}}'
    kubectl wait --for=jsonpath='{.status.phase}'=Running devbox ${devboxName} --timeout=300s
    
    if [ $? -eq 0 ]; then
        echo "Devbox $devboxName committed successfully"
    else
        echo "Failed to restart devbox $devboxName"
        return 1
    fi
}

# 检查文件是否存在
CheckFileExist() {
    local index=$1
    
    if [ -z "$index" ]; then
        echo "Usage: CheckFileExist <devbox_index>"
        return 1
    fi
    
    local devboxName="${devboxNamePrefix}-${index}"
    echo "Checking file existence in devbox: $devboxName"
    
    kubectl exec ${devboxName} -n default -- sh -c "ls -lh file.bin"
    if [ $? -eq 0 ]; then
        echo "File exists in $devboxName"
        return 0
    else
        echo "File does not exist in $devboxName"
        return 1
    fi
}

# 删除 devbox
Delete() {
    local index=$1
    
    if [ -z "$index" ]; then
        echo "Usage: Delete <devbox_index>"
        return 1
    fi
    
    local devboxName="${devboxNamePrefix}-${index}"
    echo "Deleting devbox: $devboxName"
    
    # 先停止 devbox
    kubectl patch devbox ${devboxName} -n default --type=merge -p '{"spec":{"state":"Stopped"}}'
    kubectl wait --for=jsonpath='{.status.state}'=Stopped devbox ${devboxName} --timeout=60s
    
    # 删除 devbox
    kubectl delete devbox ${devboxName} -n default --timeout=60s
    
    if [ $? -eq 0 ]; then
        echo "Devbox $devboxName deleted successfully"
    else
        echo "Failed to delete devbox $devboxName"
        return 1
    fi
}

# 执行单个 devbox 的完整测试
Test() {
    local targetDevboxIndex=$1
    
    if [ -z "$targetDevboxIndex" ]; then
        echo "Usage: Test <devbox_index>"
        return 1
    fi
    
    echo "Starting test for devbox $targetDevboxIndex"
    
    # 创建 devbox
    if ! CreateDevbox ${targetDevboxIndex}; then
        echo "Failed to create devbox $targetDevboxIndex"
        return 1
    fi
    
    # 执行命令
    if ! ExecCommand ${targetDevboxIndex} "${execCommand}"; then
        echo "Failed to execute command on devbox $targetDevboxIndex"
        return 1
    fi
    
    # 提交 devbox
    if ! Commit ${targetDevboxIndex}; then
        echo "Failed to commit devbox $targetDevboxIndex"
        return 1
    fi
    
    # 检查状态
    CheckDevboxStatus ${targetDevboxIndex}
    
    # 检查文件是否存在
    if CheckFileExist ${targetDevboxIndex}; then
        echo "Test completed successfully for devbox $targetDevboxIndex"
    else
        echo "Test failed for devbox $targetDevboxIndex - file not found after commit"
        return 1
    fi
}

# 清理所有 devbox
CleanUp() {
    echo "Starting cleanup of all devboxes..."
    
    for i in $(seq 1 $devboxCount); do
        Delete $i &
    done
    
    # 等待所有删除操作完成
    wait
    echo "Cleanup completed"
}

# 主测试函数
Main() {
    echo "Starting devbox stress test"
    echo "Configuration:"
    echo "  - Devbox count: $devboxCount"
    echo "  - Concurrency: $concurrency"
    echo "  - Command: $execCommand"
    echo "  - Name prefix: $devboxNamePrefix"
    echo ""
    
    local start_time=$(date +%s)
    local success_count=0
    local failure_count=0
    
    # 并发执行测试
    for i in $(seq 1 $devboxCount); do
        wait_for_available_slot
        echo "Starting test for devbox $i"
        Test $i &
    done
    
    # 等待所有测试完成
    wait
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    echo ""
    echo "Test completed in ${duration} seconds"
    echo "Total devboxes tested: $devboxCount"
    
    # 统计结果（这里简化处理，实际应该收集每个测试的结果）
    echo "Note: Check individual test logs for detailed results"
}

# 显示帮助信息
show_help() {
    cat << EOF
Devbox 压测脚本

用法: $0 [选项]

选项:
  -h, --help          显示帮助信息
  -c, --count         设置 devbox 数量 (默认: 10)
  -p, --prefix        设置 devbox 名称前缀 (默认: test-devbox)
  -e, --exec          设置执行命令 (默认: dd if=/dev/random of=file.bin bs=1M count=100)
  -n, --concurrency   设置并发数 (默认: 10)
  --cleanup           只执行清理
  --test <index>      测试指定的 devbox 索引

示例:
  $0                           # 执行默认测试
  $0 -c 20 -n 5                # 测试 20 个 devbox，并发数 5
  $0 --test 1                  # 只测试 devbox 1
  $0 --cleanup                 # 只清理环境

EOF
}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -c|--count)
            devboxCount="$2"
            shift 2
            ;;
        -p|--prefix)
            devboxNamePrefix="$2"
            shift 2
            ;;
        -e|--exec)
            execCommand="$2"
            shift 2
            ;;
        -n|--concurrency)
            concurrency="$2"
            shift 2
            ;;
        --cleanup)
            CleanUp
            exit 0
            ;;
        --test)
            Test "$2"
            exit $?
            ;;
        *)
            echo "未知参数: $1"
            show_help
            exit 1
            ;;
    esac
done

# 执行主测试
Main

# 询问是否清理
echo ""
read -p "测试完成，是否清理环境？(y/N): " do_cleanup
if [[ "$do_cleanup" =~ ^[Yy]$ ]]; then
    CleanUp
fi