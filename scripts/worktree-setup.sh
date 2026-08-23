#!/usr/bin/env bash
# Prepare a fresh worktree: its own Postgres databases, its own dev port.
#
# Invoked by the pre-start hook in .config/wt.toml, after `wt step copy-ignored`
# has brought .env over from the primary worktree. Safe to re-run.
set -euo pipefail

db_name="${1:?usage: worktree-setup.sh <db-name> <port>}"
port="${2:?usage: worktree-setup.sh <db-name> <port>}"
env_file="${PWD}/.env"

if [[ ! -f "$env_file" ]]; then
	echo "worktree-setup: no .env here — expected copy-ignored to run first" >&2
	exit 1
fi

host="${PGHOST:-localhost}"
port_pg="${PGPORT:-5432}"
user="${PGUSER:-postgres}"
export PGPASSWORD="${PGPASSWORD:-postgres}"

# The dev database and the test database. testsupport migrates the test one
# itself; the dev one is migrated by the post-start hook.
for db in "$db_name" "${db_name}_test"; do
	if createdb -h "$host" -p "$port_pg" -U "$user" "$db" 2>/dev/null; then
		echo "worktree-setup: created $db"
	else
		echo "worktree-setup: $db already exists"
	fi
done

marker="# --- worktrunk per-worktree overrides ---"
if grep -qF "$marker" "$env_file"; then
	echo "worktree-setup: .env already carries overrides, leaving it alone"
	exit 0
fi

# Appended rather than rewritten. Both godotenv and make let the last definition
# of a key win, so these shadow the values copied from the primary worktree
# while leaving every other secret in the file untouched.
{
	echo ""
	echo "$marker"
	echo "DATABASE_URL=postgres://${user}:${PGPASSWORD}@${host}:${port_pg}/${db_name}?sslmode=disable"
	echo "TEST_DATABASE_URL=postgres://${user}:${PGPASSWORD}@${host}:${port_pg}/${db_name}_test?sslmode=disable"
	echo "PORT=${port}"
} >>"$env_file"

echo "worktree-setup: .env points at ${db_name}, dev server on :${port}"
