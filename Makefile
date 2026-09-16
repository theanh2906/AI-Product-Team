POWERSHELL := pwsh.exe
VERSION ?=

VERSION_ARGS := $(if $(VERSION),--version $(VERSION),)
.DEFAULT_GOAL := portable

DEPLOY_VERSION_ARGS := $(if $(VERSION),-Version $(VERSION),)

.PHONY: help build portable installer installer-update deploy-installer update release all check test frontend-test go-test rust-check rust-test desktop-dev cli

help:
	@echo ProductCrew build targets
	@echo.
	@echo   make                         Build the portable package
	@echo   make portable                Build the portable package
	@echo   make installer               Build the NSIS installer package
	@echo   make update                  Bump patch version, then build and publish installer update
	@echo   make installer-update        Alias for make update
	@echo   make deploy-installer        Build installer and replace installers/ProductCrew on Cloudflare R2
	@echo   make release                 Clean dist once, then build portable, server artifact, and installer
	@echo   make all                     Alias for make release
	@echo   make VERSION=1.2.3           Build portable with a version override
	@echo   make installer VERSION=1.2.3 Build installer with a version override
	@echo   make release VERSION=1.2.3   Build all release artifacts with a version override
	@echo   make check                   Run fast compile checks
	@echo   make test                    Run frontend, Go, and Rust tests
	@echo   make cli                     Build the ProductCrew pc.exe remote-control CLI
	@echo   make desktop-dev             Start the Tauri desktop dev shell

build: portable

portable:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File ./build.ps1 $(VERSION_ARGS)

installer:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File ./build.ps1 --installer $(VERSION_ARGS)

installer-update:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File ./scripts/bump-version.ps1
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File ./build.ps1 --installer

deploy-installer:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File ./scripts/deploy-installer-r2.ps1 $(DEPLOY_VERSION_ARGS)

update: installer-update

release:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File ./build.ps1 $(VERSION_ARGS)
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File ./build.ps1 --installer --no-clean-dist $(VERSION_ARGS)

all: release

check: rust-check
	$(POWERSHELL) -NoProfile -Command "Set-Location frontend; npm test -- --watch=false"
	$(POWERSHELL) -NoProfile -Command "go test ./..."

test: frontend-test go-test rust-test

frontend-test:
	$(POWERSHELL) -NoProfile -Command "Set-Location frontend; npm test -- --watch=false"

go-test:
	$(POWERSHELL) -NoProfile -Command "go test ./..."

cli:
	$(POWERSHELL) -NoProfile -Command "New-Item -ItemType Directory -Force -Path ./dist | Out-Null; go build -o ./dist/pc.exe ./cmd/pc"

rust-check:
	$(POWERSHELL) -NoProfile -Command "Set-Location src-tauri; cargo fmt --check; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; cargo check --features installer-updates"

rust-test:
	$(POWERSHELL) -NoProfile -Command "Set-Location src-tauri; cargo test --features installer-updates"

desktop-dev:
	$(POWERSHELL) -NoProfile -Command "Set-Location frontend; npm run desktop:dev"
