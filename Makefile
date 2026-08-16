SHELL := /bin/bash
include .env
export

TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable

.PHONY: dev run migrate migrate-down sqlc css css-watch test tidy seed

tidy:
	go mod tidy

dev: css
	go run main.go

run:
	go run main.go

migrate:
	go run github.com/pressly/goose/v3/cmd/goose@latest -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	go run github.com/pressly/goose/v3/cmd/goose@latest -dir db/migrations postgres "$(DATABASE_URL)" down

# db/queries/*.sql is the source of truth for the data layer. internal/db is a
# hand-written stand-in for the first pass; run this once you want generated code.
sqlc:
	sqlc generate

css:
	npx @tailwindcss/cli -i static/css/input.css -o static/css/app.css --minify

css-watch:
	npx @tailwindcss/cli -i static/css/input.css -o static/css/app.css --watch

test:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./... -race

seed:
	go run ./cmd/seed
