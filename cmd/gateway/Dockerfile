FROM golang:latest AS coreBuilder
WORKDIR /work

RUN go install github.com/google/wire/cmd/wire@latest
COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY ./ ./

RUN wire ./cmd/gateway \
 && go build -o ccgateway ./cmd/gateway

FROM golang:latest

COPY --from=coreBuilder /work/ccgateway /usr/local/bin

CMD ["ccgateway"]
