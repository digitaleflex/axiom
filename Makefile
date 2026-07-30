.PHONY: dev build lint test clean bootstrap

dev:
	turbo dev

build:
	turbo build

lint:
	turbo lint

test:
	turbo test

clean:
	turbo clean

bootstrap:
	@echo "Running bootstrap script..."
	@if [ -f scripts/bootstrap.sh ]; then \
		scripts/bootstrap.sh; \
	elif [ -f scripts/bootstrap.ps1 ]; then \
		powershell -File scripts/bootstrap.ps1; \
	else \
		echo "No bootstrap script found."; \
	fi

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-build:
	docker compose build
