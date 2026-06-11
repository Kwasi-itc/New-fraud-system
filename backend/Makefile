.PHONY: start-up start-all postgres-up migrate migrate-up run-migrations start-services start-apis start-workers start-data-model start-data-model-worker start-ingestion start-ingestion-worker start-decision-engine start-decision-engine-worker start-screening start-screening-worker start-case-manager start-case-manager-worker

POWERSHELL := powershell -NoProfile -ExecutionPolicy Bypass
WT := wt -w 0

DATA_MODEL_DIR := data-model-service
INGESTION_DIR := ingestion-service
DECISION_ENGINE_DIR := decision-engine-service
SCREENING_DIR := screening-service
CASE_MANAGER_DIR := case-manager-service

start-up: postgres-up start-apis

start-all: postgres-up start-services

postgres-up:
	$(POWERSHELL) -Command "Push-Location '$(DATA_MODEL_DIR)'; docker compose up -d postgres; Pop-Location"

migrate: postgres-up run-migrations

migrate-up: migrate

run-migrations:
	$(POWERSHELL) -Command "$$ErrorActionPreference='Stop'; Push-Location '$(DATA_MODEL_DIR)'; New-Item -ItemType Directory -Force .bin | Out-Null; go build -o .bin/data-model-migrate.exe ./cmd/migrate; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; .\.bin\data-model-migrate.exe up; Pop-Location"
	$(POWERSHELL) -Command "$$ErrorActionPreference='Stop'; Push-Location '$(INGESTION_DIR)'; New-Item -ItemType Directory -Force .bin | Out-Null; go build -o .bin/ingestion-migrate.exe ./cmd/migrate; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; .\.bin\ingestion-migrate.exe up; Pop-Location"
	$(POWERSHELL) -Command "$$ErrorActionPreference='Stop'; Push-Location '$(DECISION_ENGINE_DIR)'; New-Item -ItemType Directory -Force .bin | Out-Null; go build -o .bin/decision-engine-migrate.exe ./cmd/migrate; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; .\.bin\decision-engine-migrate.exe up; Pop-Location"
	$(POWERSHELL) -Command "$$ErrorActionPreference='Stop'; Push-Location '$(SCREENING_DIR)'; New-Item -ItemType Directory -Force .bin | Out-Null; go build -o .bin/screening-migrate.exe ./cmd/migrate; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; .\.bin\screening-migrate.exe up; Pop-Location"
	$(POWERSHELL) -Command "$$ErrorActionPreference='Stop'; Push-Location '$(CASE_MANAGER_DIR)'; New-Item -ItemType Directory -Force .bin | Out-Null; go build -o .bin/case-manager-migrate.exe ./cmd/migrate; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; .\.bin\case-manager-migrate.exe up; Pop-Location"

start-services: start-apis start-workers

start-apis: start-data-model start-ingestion start-decision-engine start-screening start-case-manager

start-workers: start-data-model-worker start-ingestion-worker start-decision-engine-worker start-screening-worker start-case-manager-worker

start-data-model:
	$(WT) new-tab --title "data-model-api" -d "$(CURDIR)\$(DATA_MODEL_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "data-model-server.exe" -Package "./cmd/server"

start-data-model-worker:
	$(WT) new-tab --title "data-model-worker" -d "$(CURDIR)\$(DATA_MODEL_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "data-model-worker.exe" -Package "./cmd/worker"

start-ingestion:
	$(WT) new-tab --title "ingestion-api" -d "$(CURDIR)\$(INGESTION_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "ingestion-server.exe" -Package "./cmd/server"

start-ingestion-worker:
	$(WT) new-tab --title "ingestion-worker" -d "$(CURDIR)\$(INGESTION_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "ingestion-worker.exe" -Package "./cmd/worker"

start-decision-engine:
	$(WT) new-tab --title "decision-engine-api" -d "$(CURDIR)\$(DECISION_ENGINE_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "decision-engine-server.exe" -Package "./cmd/server"

start-decision-engine-worker:
	$(WT) new-tab --title "decision-engine-worker" -d "$(CURDIR)\$(DECISION_ENGINE_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "decision-engine-worker.exe" -Package "./cmd/worker"

start-screening:
	$(WT) new-tab --title "screening-api" -d "$(CURDIR)\$(SCREENING_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "screening-server.exe" -Package "./cmd/server"

start-screening-worker:
	$(WT) new-tab --title "screening-worker" -d "$(CURDIR)\$(SCREENING_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "screening-worker.exe" -Package "./cmd/worker"

start-case-manager:
	$(WT) new-tab --title "case-manager-api" -d "$(CURDIR)\$(CASE_MANAGER_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "case-manager-server.exe" -Package "./cmd/server"

start-case-manager-worker:
	$(WT) new-tab --title "case-manager-worker" -d "$(CURDIR)\$(CASE_MANAGER_DIR)" powershell -NoProfile -ExecutionPolicy Bypass -NoExit -File "$(CURDIR)\scripts\run-go-command.ps1" -BinaryName "case-manager-worker.exe" -Package "./cmd/worker"
