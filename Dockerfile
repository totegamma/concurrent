FROM golang:latest

WORKDIR /usr/src/concurrent

COPY src/* ./
RUN go mod download && go mod verify

RUN go build -v -o /usr/local/bin/concurrent ./...

CMD ["concurrent"]
