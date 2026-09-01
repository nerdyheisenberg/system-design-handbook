# syntax=docker/dockerfile:1
#
# Multi-stage build for a Go service. See Chapter 17.
#
# The point of multi-stage: the toolchain, source and module cache never reach
# the final image. A naive single-stage Go image is ~900 MB; this one is ~15 MB.
# That is not vanity — image size is pull latency on every node during a rollout,
# and every megabyte is additional attack surface to patch.

# ---------- build stage ----------
FROM golang:1.23-alpine AS build

WORKDIR /src

# Copy the manifests first and download dependencies as their own layer.
# ⭐ This layer is cached and only invalidated when go.mod/go.sum change, so
# ordinary source edits skip the dependency download entirely.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a static binary, which is what allows the scratch/
# distroless final stage below. With cgo you inherit a libc dependency and must
# ship a base image that provides it.
#
# -trimpath removes local filesystem paths from the binary, which makes builds
# reproducible and avoids leaking directory structure.
# -ldflags "-s -w" strips the symbol table and DWARF data, saving ~30%.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION:-dev}" \
      -o /out/server ./cmd/server

# ---------- runtime stage ----------
# distroless has no shell and no package manager. That removes almost every
# post-exploitation tool an attacker would otherwise find on the box.
# ⚠️ It also means you cannot `kubectl exec` into a shell to debug — use an
# ephemeral debug container instead (kubectl debug).
FROM gcr.io/distroless/static-debian12:nonroot

# ⚠️ Never run as root. If the image does not set this, the container runs as
# UID 0, and a container escape then starts from root on the host.
USER nonroot:nonroot

COPY --from=build /out/server /server

# Documentation only — EXPOSE does not publish anything.
EXPOSE 8080

# Exec form, not shell form. Shell form wraps the process in /bin/sh, which
# becomes PID 1 and does not forward SIGTERM — so your graceful shutdown never
# runs and Kubernetes kills the container after the termination grace period.
ENTRYPOINT ["/server"]
