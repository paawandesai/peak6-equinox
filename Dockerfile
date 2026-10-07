# Equinox container (M1-02). Multi-stage: a static Go build, then distroless.
# Base images are pinned by tag and digest (AB-09); bump both together.

FROM golang:1.27.1-bookworm@sha256:8d48e12ec56735e9358640898b9d9b9fcca110612ed8a5567438c0a1baa24e66 AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=readonly

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
# Embedded by internal/store (goose); serve applies them on start-up in prod and staging (M1-12).
COPY migrations ./migrations
ARG VERSION=dev
RUN go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/equinox ./cmd/equinox

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/equinox /equinox
# The collector reads config/universe/<venue>.yaml at startup (M1-06). The
# working directory is /, so the default UNIVERSE_DIR=config/universe resolves here.
COPY config /config
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/equinox"]
CMD ["serve"]
