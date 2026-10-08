# syntax=docker/dockerfile:1

# ---- build: compile a static binary ----------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/phantom-mail ./cmd/phantom-mail \
 && mkdir -p /out/data

# ---- runtime: the binary and nothing else ----------------------------------
# The image has no shell, libc, or CA bundle: the service never makes outbound
# connections. The health check is the binary itself ("phantom-mail healthcheck").
FROM scratch AS runtime
COPY --from=build /out/phantom-mail /phantom-mail
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV PM_DATA_DIR=/data \
    PM_HTTP_ADDR=:8080 \
    PM_SMTP_ADDR=:2525
VOLUME ["/data"]
EXPOSE 8080 2525
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/phantom-mail", "healthcheck"]
ENTRYPOINT ["/phantom-mail"]

# ---- test: toolchain for running the whole suite without installing anything -
# Used by "docker compose run --rm test"; the source tree is bind-mounted.
FROM node:22-bookworm AS test
RUN apt-get update && apt-get install -y --no-install-recommends jq \
 && rm -rf /var/lib/apt/lists/*
COPY --from=golang:1.23-bookworm /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH \
    GOFLAGS=-buildvcs=false \
    PM_SKIP_DOCKER_TESTS=1
WORKDIR /src
CMD ["make", "test", "docs-check"]
