# The CSS has to be built before the Go binary, not after: app.css is
# gitignored and assets/assets.go embeds the whole static tree at compile
# time, so a Go build that runs first bakes in a missing or stale
# stylesheet. Tailwind scans the .templ sources for class names, which is
# why they are copied in here rather than only in the Go stage.
FROM node:22-slim AS css

WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY assets/static/css/input.css ./assets/static/css/input.css
COPY assets/static/js ./assets/static/js
COPY internal/ui ./internal/ui
RUN npx @tailwindcss/cli -i assets/static/css/input.css -o assets/static/css/app.css --minify


FROM golang:1.27 AS build

WORKDIR /src
# Dependencies resolve in their own layer so a source-only change doesn't
# re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=css /src/assets/static/css/app.css ./assets/static/css/app.css

# The generated *_templ.go files are committed, so this is normally a no-op.
# Regenerating anyway means the image is built from the .templ sources even
# if a commit ever lands with stale output; CI is what fails on that drift,
# the image just shouldn't ship it.
RUN go tool templ generate

# CGO off gives a static binary, which is what lets the runtime stage be
# distroless. Trimming paths and symbols keeps the image small; nothing here
# is debugged by attaching to the container.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/rankanything ./cmd/rankanything


# Distroless rather than alpine: the binary is static and the only other
# things it needs are CA certificates, for Resend's HTTPS API and for a
# Postgres connection using sslmode=require.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/rankanything /rankanything

# Documentation only — Render injects its own PORT and the app reads it.
EXPOSE 8001
USER nonroot:nonroot
ENTRYPOINT ["/rankanything"]
