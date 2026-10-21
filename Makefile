.PHONY: setup up down logs migrate seed test lint demo clean integration
setup:
	@test -f .env || cp .env.example .env
up: setup
	docker compose up -d --build --wait
down:
	docker compose down
logs:
	docker compose logs -f api worker
migrate:
	docker compose run --rm migrate
seed:
	docker compose run --rm simulator -url http://api:8080 -seed-only
demo:
	docker compose run --rm simulator -url http://api:8080 -scenario latency-spike
test:
	go test ./...
	cd web && npm test
lint:
	@test -z "$$(gofmt -l cmd internal migrations tests)"
	go vet ./...
	cd web && npm run lint && npm run build
integration:
	sh scripts/integration.sh
clean:
	@echo 'To permanently delete local data, run: docker compose down -v'
