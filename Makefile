.DEFAULT_GOAL := help

WEB_DIR := web
SERVER_DIR := server

.PHONY: help
help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: install
install: ## 安装前后端依赖
	cd $(WEB_DIR) && npm install
	cd $(SERVER_DIR) && go mod download

.PHONY: dev
dev: ## 后台启动/重启前后端（重编译后端；scripts/dev.sh restart all）
	scripts/dev.sh restart all

.PHONY: stop
stop: ## 停止前后端开发服务
	scripts/dev.sh stop all

.PHONY: status
status: ## 查看前后端开发服务状态
	scripts/dev.sh status

.PHONY: preview
preview: ## 一条命令起全栈供预览（后端后台常驻，前端前台跑日志）
	scripts/dev.sh restart server
	scripts/dev.sh stop web
	npm run dev --prefix web

.PHONY: dev-web
dev-web: ## 前台启动前端开发服务器 (http://localhost:45173)
	cd $(WEB_DIR) && npm run dev

.PHONY: dev-server
dev-server: ## 前台启动后端开发服务器 (http://127.0.0.1:48080)
	cd $(SERVER_DIR) && ACP_DEBUG=1 go run ./cmd/server

.PHONY: build
build: build-web build-server ## 构建前后端

.PHONY: build-web
build-web: ## 构建前端到 build/web
	cd $(WEB_DIR) && npm run build

.PHONY: build-server
build-server: ## 构建后端到 build/server/acp-server
	cd $(SERVER_DIR) && go build -o ../build/server/acp-server ./cmd/server

.PHONY: serve
serve: build-web build-server ## 由后端单进程托管前端产物
	ACP_WEB_DIR=build/web ./build/server/acp-server

.PHONY: serve-lan
serve-lan: build-web build-server ## 同 serve，但对局域网开放（分享链接才打得开）
	@echo "局域网访问已开启——分享链接对局域网内设备有效。工作区终端是任意命令执行面，只在可信网络里用（README §安全姿态）。"
	ACP_WEB_DIR=build/web ACP_ADDR=0.0.0.0:48080 ./build/server/acp-server

.PHONY: app
app: ## 打包 macOS 桌面版到 build/app/ACPP.app
	scripts/build-macos-app.sh

.PHONY: release
release: ## 发布桌面版到 GitHub Releases（默认上个 tag 逢十进位：0.5.9→0.6.0；跳版本用 make release VERSION=x.y.z）
	scripts/release-macos.sh $(VERSION)

.PHONY: check
check: lint typecheck test check-structure ## 全部验证：lint + typecheck + test + 结构检查

.PHONY: check-structure
check-structure: ## 结构检查：行数/目录文件数硬线、禁止模式、工具索引对账
	scripts/check-structure.sh

.PHONY: lint
lint: ## 前端 eslint + 后端 go vet
	cd $(WEB_DIR) && npm run lint
	cd $(SERVER_DIR) && go vet ./...

.PHONY: test
test: ## 运行后端测试
	cd $(SERVER_DIR) && go test ./...

.PHONY: typecheck
typecheck: ## 前端类型检查
	cd $(WEB_DIR) && npm run typecheck

.PHONY: clean
clean: ## 清理构建产物与本地数据库
	rm -rf build $(SERVER_DIR)/data/*.db
