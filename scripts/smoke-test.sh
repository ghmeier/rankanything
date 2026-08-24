#!/usr/bin/env bash
# Post-deploy smoke test: prove a running deploy actually serves, rather
# than merely that the container started.
#
#   scripts/smoke-test.sh https://rankanything.onrender.com
#
# Checks only unauthenticated surfaces, so it is safe to run against
# production as often as you like: it creates no account, writes no rows,
# and sends no email. What it is really guarding against is the class of
# failure where the binary boots and answers /healthz but the embedded
# assets or templ components didn't make it into the image.
set -euo pipefail

base_url="${1:?usage: smoke-test.sh <base-url>}"
base_url="${base_url%/}"

failures=0

# check <description> <path> <expected-status> [expected-substring]
check() {
	local description="$1" path="$2" want_status="$3" want_body="${4:-}"
	local body status

	# Write the body to a file and the status to stdout, so a body
	# containing digits can't be confused for the status code.
	body="$(mktemp)"
	status="$(curl --silent --show-error --location --max-time 20 \
		--output "$body" --write-out '%{http_code}' "${base_url}${path}" || echo 000)"

	if [[ "$status" != "$want_status" ]]; then
		echo "FAIL  ${description}: ${path} returned ${status}, want ${want_status}"
		failures=$((failures + 1))
		rm -f "$body"
		return
	fi

	if [[ -n "$want_body" ]] && ! grep -q -- "$want_body" "$body"; then
		echo "FAIL  ${description}: ${path} is ${status} but does not contain '${want_body}'"
		failures=$((failures + 1))
		rm -f "$body"
		return
	fi

	echo "ok    ${description}"
	rm -f "$body"
}

echo "Smoke testing ${base_url}"

# The database is reachable from inside the container. Everything below
# would fail confusingly if this doesn't hold, so it goes first.
check "health check"        /healthz     200 "ok"

# The landing page proves templ components render, not just that routes are
# mounted: this markup is compiled into the binary.
check "landing page"        /            200 "Rank anything."
check "sign-in page"        /login       200 "Sign in"
check "registration page"   /register    200

# The static tree is embedded at compile time. Serving the stylesheet proves
# the container's CSS build stage ran and its output was embedded — the one
# thing that is built differently in the image than it is locally.
check "stylesheet"          /static/css/app.css 200 "--app-background"
check "board script"        /static/js/board.js 200

check "robots.txt"          /robots.txt  200 "User-agent"
check "sitemap.xml"         /sitemap.xml 200 "<urlset"

# The component gallery is a development tool. Finding it in production
# means APP_ENV was not set, which also means secure cookies are off.
check "gallery is not exposed" /components 404

# An unknown share slug must 404 rather than error, since this is the one
# public route that takes untrusted input straight from the URL.
check "unknown share link 404s" /s/definitely-not-a-real-slug 404

if (( failures > 0 )); then
	echo
	echo "${failures} check(s) failed against ${base_url}"
	exit 1
fi

echo
echo "All checks passed against ${base_url}"
