FROM node:26-slim AS css

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

# Trimming paths and symbols keeps the image small.
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
