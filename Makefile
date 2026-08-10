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

.PHONY: dev-web
dev-web: ## 启动前端开发服务器 (http://localhost:5173)
	cd $(WEB_DIR) && npm run dev

.PHONY: dev-server
dev-server: ## 启动后端开发服务器 (http://127.0.0.1:8080)
	cd $(SERVER_DIR) && ACP_DEBUG=1 go run ./cmd/server

.PHONY: build
build: build-web build-server ## 构建前后端

.PHONY: build-web
build-web: ## 构建前端到 web/dist
	cd $(WEB_DIR) && npm run build

.PHONY: build-server
build-server: ## 构建后端到 server/bin/acp-server
	cd $(SERVER_DIR) && go build -o bin/acp-server ./cmd/server

.PHONY: serve
serve: build-web build-server ## 由后端单进程托管前端产物
	cd $(SERVER_DIR) && ACP_WEB_DIR=../$(WEB_DIR)/dist ./bin/acp-server

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
	rm -rf $(WEB_DIR)/dist $(SERVER_DIR)/bin $(SERVER_DIR)/data/*.db
