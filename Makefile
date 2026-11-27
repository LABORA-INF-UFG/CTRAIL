# CTRAIL Gateway - Comandos Simples

help: ## Mostrar esta ajuda
	@echo "CTRAIL Gateway - Comandos disponíveis:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Construir a imagem Docker
	docker compose build

run: ## Executar o container
	docker compose up

start: ## Executar em background
	docker compose up -d

stop: ## Parar o container
	docker compose down

restart: ## Reiniciar o container
	docker compose restart

logs: ## Mostrar logs do container
	docker compose logs -f

shell: ## Acessar shell do container
	docker compose exec ctrail sh

clean: ## Remover container e imagens
	docker compose down --rmi all

health: ## Verificar saúde do container
	docker compose exec ctrail wget --spider -q http://localhost:8080/health && echo "✅ Saudável" || echo "❌ Com problemas"

ps: ## Mostrar containers rodando
	docker compose ps

stats: ## Mostrar estatísticas do container
	docker stats ctrail

build-mcp: ## Compilar o adaptador MCP
	go build -o bin/ctrail-mcp ./cmd/mcp

test-mcp: ## Testar a integração MCP
	go test ./internal/mcpserver/... ./cmd/mcp

.PHONY: help build run start stop restart logs shell clean health ps stats build-mcp test-mcp
