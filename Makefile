.PHONY: help tools dev run build test fmt lint migrate-up migrate-down migrate-status clean deps seed

BIN        := yuhada
GO_FILES   := $(shell find . -name '*.go' -not -path './.archive/*' -not -path './vendor/*')
MODULE     := github.com/mingicho/yuhada

# ----- Linux 크로스 컴파일 타겟 (Cafe24 VPS x86_64) -----
LINUX_ARCH := amd64

# ----- go install 된 툴 PATH -----
GOBIN      := $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

help: ## 사용 가능한 타겟 목록
	@awk 'BEGIN {FS=":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Go 의존성 정리
	go mod tidy

tools: ## 개발 툴 설치 (templ, sqlc, goose, tailwind)
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest
	@./scripts/install-tailwind.sh

run: ## 로컬 실행 (빌드 없이 go run)
	APP_ENV=dev go run ./cmd/server

dev: templ css ## 개발 모드 (templ generate + css build + go run)
	@$(MAKE) run

templ: ## templ → Go 코드 생성
	templ generate

templ-watch: ## templ watch (백그라운드)
	templ generate --watch --proxy=http://localhost:8080

css: ## Tailwind CSS 빌드
	@./tailwind/bin/tailwindcss -i ./tailwind/input.css -o ./static/css/app.css --minify

css-watch: ## Tailwind watch
	@./tailwind/bin/tailwindcss -i ./tailwind/input.css -o ./static/css/app.css --watch

build: ## Linux amd64 크로스 컴파일
	CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_ARCH) \
	  go build -trimpath -ldflags="-s -w" -o dist/$(BIN) ./cmd/server
	@ls -lh dist/$(BIN)

build-mac: ## macOS (로컬 테스트용)
	go build -trimpath -o dist/$(BIN) ./cmd/server

fmt: ## 포매팅
	gofmt -w $(GO_FILES)

test: ## 테스트
	go test ./...

# ----- Migrations -----
DB_PATH ?= ./var/yuhada.db

migrate-up: ## goose up (마이그레이션 전체 적용)
	@mkdir -p $(dir $(DB_PATH))
	goose -dir migrations sqlite3 $(DB_PATH) up

migrate-down: ## goose down (1 step)
	goose -dir migrations sqlite3 $(DB_PATH) down

migrate-status: ## goose status
	goose -dir migrations sqlite3 $(DB_PATH) status

migrate-reset: ## DB 삭제 후 전체 재적용 (dev 전용!)
	rm -f $(DB_PATH) $(DB_PATH)-wal $(DB_PATH)-shm
	@$(MAKE) migrate-up

seed: ## 시드 데이터 삽입
	sqlite3 $(DB_PATH) < scripts/seed.sql
	@echo "✓ seed data inserted"

# ----- 정리 -----
clean: ## 빌드 산출물 제거
	rm -rf dist
	rm -f var/*.db var/*.db-wal var/*.db-shm
