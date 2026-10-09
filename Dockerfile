# Equinox container (M1-02). Multi-stage: a static Go build, then distroless.
# Base images are pinned by tag and digest (AB-09); bump both together.

FROM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
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
# The collector reads config/universe/<venue>.yaml at startup (M1-06). COPY keeps
# the build host's file modes, and a checkout made with a strict umask (0600
# files, 0700 directories) would be unreadable by the nonroot runtime user, so
# the modes are normalised here (M1-12).
COPY config /out/config
RUN chmod -R a+rX /out/config

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/equinox /equinox
COPY --from=build /out/config /config
# The nonroot base image's working directory is /home/nonroot; the default
# UNIVERSE_DIR=config/universe is relative, so run from / where config/ lives.
WORKDIR /
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/equinox"]
CMD ["serve"]
