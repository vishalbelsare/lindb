# Build web
FROM node:18.12.1 as web_builder

WORKDIR /web_workspace
COPY web/ web/
COPY Makefile Makefile
RUN make build-frontend

# Build the manager binary
FROM golang:1.19 as go_builder
ARG TARGETOS
ARG TARGETARCH
ARG LD_FLAGS

WORKDIR /go_workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy src code
COPY . .

# Copy web static resource
COPY --from=web_builder /web_workspace/web/static/ web/static

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN	CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build "${LD_FLAGS}" -o lind ./cmd/lind \
    && go build "${LD_FLAGS}" -o lindctl ./cmd/cli

FROM centos:latest
WORKDIR /
COPY --from=go_builder /go_workspace/lind /usr/bin/lind
COPY --from=go_builder /go_workspace/lindctl /usr/bin/lindctl
RUN ln -s /usr/bin/lind /usr/local/bin/lind
RUN ln -s /usr/bin/lindctl /usr/local/bin/lindctl
RUN mkdir /etc/lindb
RUN mkdir /data

USER 0:0
