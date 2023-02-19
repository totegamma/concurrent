FROM golang:latest

WORKDIR /usr/src/concurrent

COPY src/go.mod src/go.sum ./
RUN go mod download && go mod verify

COPY src/ ./
RUN go build -v -o /usr/local/bin/concurrent

CMD ["concurrent"]
