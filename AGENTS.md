## Project Outline

Rankanything is an application that allows users to create
ranking tier lists, add items to those lists, and share them
with others. It provides a straightforward, performant, and
secure interface to create and manage rankings.

The main advantage over other such applications is its user-friendliness, interoperability, and concern for privacy.

## Tech Stack

- Go 1.26
- HTMX 3
- Tailwindcss 4
- Postgres 18

Within go, it utilizes goose, pgx, and sqlc to interfacer with
the database.

## Commands

All common commands are stored in `Makefile`. Most commonly:

```
# Align dependencies
make tidy

# Run tests
make test

# Run the server
make run
``
```
