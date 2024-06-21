FROM golang:latest AS coreBuilder
WORKDIR /work

ARG VERSION

COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY ./ ./

RUN VERSION=${VERSION:-$(git describe)} \
 && BUILD_MACHINE=$(uname -srmo) \
 && BUILD_TIME=$(date) \
 && GO_VERSION=$(go version) \
 && go build -ldflags "-s -w -X main.version=${VERSION} -X \"main.buildMachine=${BUILD_MACHINE}\" -X \"main.buildTime=${BUILD_TIME}\" -X \"main.goVersion=${GO_VERSION}\"" -o ccgateway ./cmd/gateway

FROM ubuntu:latest
RUN apt-get update && apt-get install -y ca-certificates curl --no-install-recommends && rm -rf /var/lib/apt/lists/*

COPY --from=coreBuilder /work/ccgateway /usr/local/bin

CMD ["ccgateway"]
