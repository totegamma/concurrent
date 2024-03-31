FROM golang:latest AS coreBuilder
WORKDIR /work

COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY ./ ./

RUN wire ./cmd/gateway \
 && go build -ldflags "-s -w -X main.version=$(git describe) -X \"main.buildMachine=$(uname -srmo)\" -X \"main.buildTime=$(date)\" -X \"main.goVersion=$(go version)\"" -o ccgateway ./cmd/gateway

FROM golang:latest

COPY --from=coreBuilder /work/ccgateway /usr/local/bin

CMD ["ccgateway"]
