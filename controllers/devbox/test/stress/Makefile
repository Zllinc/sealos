# Devbox 压测工具 Makefile

.PHONY: help build test clean scale concurrent capacity stress all monitor burner-install burner-quick burner-scale burner-concurrent burner-all burner-cleanup

# 默认配置
NAMESPACE ?= devbox-stress-test
IMAGE ?= ubuntu:22.04
CPU ?= 100m
MEMORY ?= 128Mi
STORAGE ?= 1Gi
KUBECONFIG ?= $(HOME)/.kube/config

# 构建输出
BINARY = devbox-stress-test
GO_FILES = devbox_stress_test.go

help: ## 显示帮助信息
	@echo "Devbox 压测工具"
	@echo ""
	@echo "使用方法:"
	@echo "  make [target] [VARIABLE=value]"
	@echo ""
	@echo "目标 (Targets):"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""
	@echo "变量 (Variables):"
	@echo "  NAMESPACE   测试命名空间 (默认: $(NAMESPACE))"
	@echo "  IMAGE       devbox 镜像 (默认: $(IMAGE))"
	@echo "  CPU         CPU 资源 (默认: $(CPU))"
	@echo "  MEMORY      内存资源 (默认: $(MEMORY))"
	@echo "  STORAGE     存储资源 (默认: $(STORAGE))"
	@echo "  KUBECONFIG  kubeconfig 路径 (默认: $(KUBECONFIG))"
	@echo ""
	@echo "示例:"
	@echo "  make build                    # 构建压测工具"
	@echo "  make scale NAMESPACE=test     # 在 test 命名空间执行规模测试"
	@echo "  make all CPU=200m MEMORY=256Mi # 使用自定义资源配置执行所有测试"

build: ## 构建压测工具
	@echo "构建压测工具..."
	@go mod init devbox-stress-test 2>/dev/null || true
	@go mod tidy
	@go build -o $(BINARY) $(GO_FILES)
	@chmod +x $(BINARY)
	@chmod +x *.sh
	@echo "构建完成: $(BINARY)"

test: build ## 检查工具是否正常工作
	@echo "检查压测工具..."
	@./$(BINARY) -help || true
	@echo "工具检查完成"

clean: ## 清理构建产物和测试结果
	@echo "清理构建产物和测试结果..."
	@rm -f $(BINARY)
	@rm -rf stress_test_results/
	@rm -rf stress_test_final_report_*/
	@rm -f go.mod go.sum
	@echo "清理完成"

setup: ## 设置测试环境
	@echo "设置测试环境..."
	@kubectl create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	@echo "测试环境设置完成"

# 具体测试场景

scale: build ## 执行规模测试 (100, 300, 500, 1000 个 devbox)
	@echo "执行规模测试..."
	@./run_stress_tests.sh \
		--namespace $(NAMESPACE) \
		--image $(IMAGE) \
		--cpu $(CPU) \
		--memory $(MEMORY) \
		--storage $(STORAGE) \
		scale

concurrent: build ## 执行并发测试 (5, 10, 20, 50 并发)
	@echo "执行并发测试..."
	@./run_stress_tests.sh \
		--namespace $(NAMESPACE) \
		--image $(IMAGE) \
		--cpu $(CPU) \
		--memory $(MEMORY) \
		--storage $(STORAGE) \
		concurrent

capacity: build ## 执行容量测试 (寻找集群上限)
	@echo "执行容量测试..."
	@./run_stress_tests.sh \
		--namespace $(NAMESPACE) \
		--image $(IMAGE) \
		--cpu $(CPU) \
		--memory $(MEMORY) \
		--storage $(STORAGE) \
		capacity

stress: build ## 执行压力测试 (1小时持续负载)
	@echo "执行压力测试..."
	@./run_stress_tests.sh \
		--namespace $(NAMESPACE) \
		--image $(IMAGE) \
		--cpu $(CPU) \
		--memory $(MEMORY) \
		--storage $(STORAGE) \
		stress

all: build ## 执行所有测试
	@echo "执行所有压测..."
	@./run_stress_tests.sh \
		--namespace $(NAMESPACE) \
		--image $(IMAGE) \
		--cpu $(CPU) \
		--memory $(MEMORY) \
		--storage $(STORAGE) \
		all

monitor: ## 启动资源监控 (持续30分钟)
	@echo "启动资源监控..."
	@./monitor.sh 1800 30

# 快速测试场景

quick-scale: build ## 快速规模测试 (10, 20, 50 个 devbox)
	@echo "执行快速规模测试..."
	@for count in 10 20 50; do \
		echo "测试 $$count 个 devbox..."; \
		./$(BINARY) \
			-kubeconfig=$(KUBECONFIG) \
			-namespace=$(NAMESPACE) \
			-test-type=scale \
			-devbox-count=$$count \
			-create-interval=1s \
			-timeout=10m \
			-image=$(IMAGE) \
			-cpu=$(CPU) \
			-memory=$(MEMORY) \
			-storage=$(STORAGE) \
			-cleanup=true; \
		sleep 30; \
	done

quick-concurrent: build ## 快速并发测试 (5, 10 并发创建 20 个 devbox)
	@echo "执行快速并发测试..."
	@for concurrent in 5 10; do \
		echo "测试 $$concurrent 并发..."; \
		./$(BINARY) \
			-kubeconfig=$(KUBECONFIG) \
			-namespace=$(NAMESPACE) \
			-test-type=concurrent \
			-devbox-count=20 \
			-concurrent=$$concurrent \
			-timeout=10m \
			-image=$(IMAGE) \
			-cpu=$(CPU) \
			-memory=$(MEMORY) \
			-storage=$(STORAGE) \
			-cleanup=true; \
		sleep 30; \
	done

# 清理和维护

cleanup: ## 清理测试环境
	@echo "清理测试环境..."
	@kubectl delete namespace $(NAMESPACE) --ignore-not-found=true
	@echo "测试环境清理完成"

status: ## 查看当前测试状态
	@echo "=== 集群状态 ==="
	@kubectl get nodes
	@echo ""
	@echo "=== Devbox 状态 ==="
	@kubectl get devbox -A 2>/dev/null || echo "未找到 devbox"
	@echo ""
	@echo "=== Pod 状态 ==="
	@kubectl get pods -n $(NAMESPACE) 2>/dev/null || echo "命名空间 $(NAMESPACE) 不存在"

logs: ## 查看最近的测试日志
	@echo "=== 最近的测试结果 ==="
	@find ./stress_test_results -name "*.log" -exec echo "=== {} ===" \; -exec tail -10 {} \; 2>/dev/null || echo "未找到测试结果"

# 性能调优测试

tune-cpu: build ## CPU 资源调优测试
	@echo "执行 CPU 资源调优测试..."
	@for cpu in 50m 100m 200m 500m; do \
		echo "测试 CPU: $$cpu"; \
		./$(BINARY) \
			-kubeconfig=$(KUBECONFIG) \
			-namespace=$(NAMESPACE) \
			-test-type=scale \
			-devbox-count=50 \
			-create-interval=2s \
			-timeout=15m \
			-image=$(IMAGE) \
			-cpu=$$cpu \
			-memory=$(MEMORY) \
			-storage=$(STORAGE) \
			-cleanup=true; \
		sleep 60; \
	done

tune-memory: build ## 内存资源调优测试
	@echo "执行内存资源调优测试..."
	@for memory in 64Mi 128Mi 256Mi 512Mi; do \
		echo "测试内存: $$memory"; \
		./$(BINARY) \
			-kubeconfig=$(KUBECONFIG) \
			-namespace=$(NAMESPACE) \
			-test-type=scale \
			-devbox-count=50 \
			-create-interval=2s \
			-timeout=15m \
			-image=$(IMAGE) \
			-cpu=$(CPU) \
			-memory=$$memory \
			-storage=$(STORAGE) \
			-cleanup=true; \
		sleep 60; \
	done

# 生成报告

report: ## 生成测试报告
	@echo "生成测试报告..."
	@report_dir="stress_test_report_$(shell date +%Y%m%d_%H%M%S)"; \
	mkdir -p $$report_dir; \
	echo "# Devbox 压测报告" > $$report_dir/README.md; \
	echo "" >> $$report_dir/README.md; \
	echo "生成时间: $(shell date)" >> $$report_dir/README.md; \
	echo "测试配置:" >> $$report_dir/README.md; \
	echo "- 命名空间: $(NAMESPACE)" >> $$report_dir/README.md; \
	echo "- 镜像: $(IMAGE)" >> $$report_dir/README.md; \
	echo "- CPU: $(CPU)" >> $$report_dir/README.md; \
	echo "- 内存: $(MEMORY)" >> $$report_dir/README.md; \
	echo "- 存储: $(STORAGE)" >> $$report_dir/README.md; \
	find ./stress_test_results -name "*.log" -o -name "*.md" 2>/dev/null | xargs -I {} cp {} $$report_dir/ || true; \
	echo "报告已生成: $$report_dir"

# 开发辅助

fmt: ## 格式化 Go 代码
	@go fmt ./...

vet: ## 检查 Go 代码
	@go vet ./...

deps: ## 安装依赖
	@go mod download
	@go mod tidy

# 信息查看

version: ## 显示版本信息
	@echo "Devbox 压测工具 v1.0"
	@echo "Go 版本: $(shell go version)"
	@echo "Kubectl 版本: $(shell kubectl version --client --short 2>/dev/null || echo '未安装')"

check: ## 检查环境
	@echo "检查测试环境..."
	@echo "Kubectl: $(shell command -v kubectl || echo '未安装')"
	@echo "Go: $(shell command -v go || echo '未安装')"
	@echo "集群连接: $(shell kubectl cluster-info 2>/dev/null | head -1 || echo '无法连接')"
	@echo "Devbox CRD: $(shell kubectl get crd devboxes.devbox.sealos.io 2>/dev/null | tail -1 || echo '未安装')"
	@echo "Kube-burner: $(shell command -v kube-burner || echo '未安装')"

# === Kube-burner 工具 ===

burner-install: ## 安装 kube-burner 工具
	@echo "安装 kube-burner..."
	@chmod +x run-kube-burner.sh
	@./run-kube-burner.sh --install

burner-quick: ## 使用 kube-burner 快速测试 (20 个 devbox)
	@echo "使用 kube-burner 执行快速测试..."
	@chmod +x run-kube-burner.sh
	@./run-kube-burner.sh quick

burner-scale: ## 使用 kube-burner 规模测试 (100 个 devbox)
	@echo "使用 kube-burner 执行规模测试..."
	@chmod +x run-kube-burner.sh
	@./run-kube-burner.sh scale

burner-concurrent: ## 使用 kube-burner 并发测试 (50 个 devbox, 高并发)
	@echo "使用 kube-burner 执行并发测试..."
	@chmod +x run-kube-burner.sh
	@./run-kube-burner.sh concurrent

burner-all: ## 使用 kube-burner 完整测试
	@echo "使用 kube-burner 执行完整测试..."
	@chmod +x run-kube-burner.sh
	@./run-kube-burner.sh all

burner-cleanup: ## 清理 kube-burner 测试环境
	@echo "清理 kube-burner 测试环境..."
	@chmod +x run-kube-burner.sh
	@./run-kube-burner.sh --cleanup

commit-stopped: build ## 测试 Stopped 状态 commit 操作
	@echo "测试 Stopped 状态 commit 操作..."
	./$(BINARY_NAME) commit stopped --count 5 --concurrent 3 --file-count 20 $(STRESS_FLAGS)

commit-shutdown: build ## 测试 Shutdown 状态 commit 操作
	@echo "测试 Shutdown 状态 commit 操作..."
	./$(BINARY_NAME) commit shutdown --count 5 --concurrent 3 --file-count 20 $(STRESS_FLAGS)

commit-stopped-large: build ## 大规模 Stopped 状态 commit 测试
	@echo "大规模 Stopped 状态 commit 测试..."
	./$(BINARY_NAME) commit stopped --count 10 --concurrent 5 --file-count 50 $(STRESS_FLAGS)

commit-shutdown-large: build ## 大规模 Shutdown 状态 commit 测试
	@echo "大规模 Shutdown 状态 commit 测试..."
	./$(BINARY_NAME) commit shutdown --count 10 --concurrent 5 --file-count 50 $(STRESS_FLAGS)

delete-test: build ## 测试 devbox 删除和资源清理
	@echo "测试 devbox 删除和资源清理..."
	./devbox-stress delete --count 3 --verify --namespace $(NAMESPACE)

delete-test-large: build ## 大规模删除测试 (10 个 devbox)
	@echo "大规模删除测试..."
	./devbox-stress delete --count 10 --verify --namespace $(NAMESPACE)

delete: build ## 删除测试 - 验证 devbox 删除后所有资源都被清理
	@echo "删除测试 - 验证 devbox 删除后所有资源都被清理..."
	./devbox-stress delete --count 5 --verify --namespace $(NAMESPACE)

lvm-status: build ## 检查 LVM 状态
	@echo "检查 LVM 状态..."
	./devbox-stress lvm status --namespace $(NAMESPACE)

lvm-cleanup: build ## 清理 LVM 资源
	@echo "清理 LVM 资源..."
	./devbox-stress lvm cleanup --namespace $(NAMESPACE)

# pprof 性能分析测试
pprof-test: build ## 运行 pprof 性能分析测试
	@echo "运行 pprof 性能分析..."
	./devbox-stress pprof --count 5 --namespace $(NAMESPACE)

pprof-concurrent: build ## 运行并发 pprof 性能分析
	@echo "运行并发 pprof 性能分析..."
	./devbox-stress pprof --count 10 --concurrent 3 --namespace $(NAMESPACE)

pprof-web: build ## 运行带 web 服务器的 pprof 分析
	@echo "运行带 web 服务器的 pprof 分析..."
	./devbox-stress pprof --count 5 --web --port 6060 --namespace $(NAMESPACE)

pprof-large: build ## 运行大规模 pprof 性能分析
	@echo "运行大规模 pprof 性能分析..."
	./devbox-stress pprof --count 20 --concurrent 5 --output large_pprof_report.txt --namespace $(NAMESPACE)

pprof-example: ## 运行 pprof 示例
	@echo "运行 pprof 示例..."
	go run pprof_example.go

# 清理 profile 文件
clean-profiles: ## 清理生成的 profile 文件
	@echo "清理 profile 文件..."
	rm -rf ./profiles/

# 小文件 commit 测试
smallfile-test: build ## 运行小文件 commit 测试
	@echo "运行小文件 commit 测试..."
	./devbox-stress smallfile --count 5000 --total-size 1G --concurrent 5 --namespace $(NAMESPACE)

smallfile-large: build ## 运行大规模小文件测试
	@echo "运行大规模小文件测试..."
	./devbox-stress smallfile --count 10000 --total-size 2G --concurrent 10 --namespace $(NAMESPACE)

smallfile-custom: build ## 自定义小文件测试
	@echo "自定义小文件测试..."
	./devbox-stress smallfile --count $(SMALLFILE_COUNT) --total-size $(SMALLFILE_SIZE) --concurrent $(SMALLFILE_CONCURRENT) --namespace $(NAMESPACE)
