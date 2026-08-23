SHELL := /bin/bash

# Optional so the make targets CI calls (css, templ) still work where no
# .env exists. Locally it is what points each worktree at its own database
# and port; without it the database-backed tests skip and make test passes
# vacuously.
-include .env
export

.PHONY: dev run migrate migrate-down sqlc css css-watch templ templ-watch test tidy seed

tidy:
	go mod tidy

dev:
	air --proxy.enabled=true --proxy.app_port="${PORT}"

watch:
	air

templ:
	go tool templ generate

templ-watch:
	go tool templ generate --watch

run:
	go run cmd/rankanything/main.go

migrate:
	go run github.com/pressly/goose/v3/cmd/goose@latest -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	go run github.com/pressly/goose/v3/cmd/goose@latest -dir db/migrations postgres "$(DATABASE_URL)" down

sqlc:
	sqlc generate

css:
	npx @tailwindcss/cli -i assets/static/css/input.css -o assets/static/css/app.css --minify

css-watch:
	npx @tailwindcss/cli -i assets/static/css/input.css -o assets/static/css/app.css --watch

test:
	go test ./... -race
