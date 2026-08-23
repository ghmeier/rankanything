SHELL := /bin/bash
include .env
export

TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable

.PHONY: dev run migrate migrate-down sqlc css css-watch test tidy seed

tidy:
	go mod tidy

dev:
	@trap 'kill 0' INT TERM EXIT; \
	$(MAKE) watch & \
	$(MAKE) css-watch

watch:
	air

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
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" rtk go test ./... -race
