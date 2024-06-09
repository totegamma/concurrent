FROM golang:latest AS coreBuilder
WORKDIR /work

COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY ./ ./

RUN go build -ldflags "-s -w -X main.version=$(git describe) -X \"main.buildMachine=$(uname -srmo)\" -X \"main.buildTime=$(date)\" -X \"main.goVersion=$(go version)\"" -o ccapi ./cmd/api

FROM ubuntu:latest

COPY --from=coreBuilder /work/ccapi /usr/local/bin

CMD ["ccapi"]
