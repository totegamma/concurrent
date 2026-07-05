FROM golang:latest AS builder
WORKDIR /work

COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY ./ ./
RUN go build -ldflags "-s -w" -o k8s-elector ./cmd/k8s-elector

# ---------------------------------
FROM ubuntu:latest AS certificates

RUN apt-get update && apt-get install -y ca-certificates

# ---------------------------------
FROM ubuntu:latest

COPY --from=builder /work/k8s-elector /usr/local/bin
COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

CMD ["k8s-elector"]
