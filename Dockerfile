# Build the manager binary
FROM golang:1.15 AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go
RUN git clone https://github.com/SandeepPissay/helm-charts.git wcp-elk && cd wcp-elk && git checkout -t remotes/origin/wcp-elk

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM ubuntu:20.04
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/wcp-elk/ wcp-elk/
#USER 65532:65532

ENTRYPOINT ["/manager"]
