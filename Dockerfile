# syntax=docker/dockerfile:1

# ---- build stage ----------------------------------------------------------
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/containerdrift ./cmd/containerdrift

# ---- runtime stage --------------------------------------------------------
# ContainerDrift runs "docker compose config", so the runtime image needs the
# Docker CLI with the Compose plugin. The official docker:cli image is meant to
# provide both; the build fails here if the plugin is missing.
FROM docker:cli
COPY --from=build /out/containerdrift /usr/local/bin/containerdrift
RUN docker compose version
WORKDIR /work
ENTRYPOINT ["containerdrift"]
CMD ["--help"]
