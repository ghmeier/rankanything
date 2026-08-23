#!/usr/bin/env bash
# Drop the databases a worktree owned. Invoked by the post-remove hook in
# .config/wt.toml, which runs from the primary worktree after the branch's
# worktree is already gone.
set -euo pipefail

db_name="${1:?usage: worktree-teardown.sh <db-name>}"

host="${PGHOST:-localhost}"
port_pg="${PGPORT:-5432}"
user="${PGUSER:-postgres}"
export PGPASSWORD="${PGPASSWORD:-postgres}"

for db in "$db_name" "${db_name}_test"; do
	dropdb -h "$host" -p "$port_pg" -U "$user" --if-exists "$db"
	echo "worktree-teardown: dropped $db"
done
