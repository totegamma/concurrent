FROM golang:latest AS builder

WORKDIR /work

COPY src/go.mod src/go.sum ./
RUN go mod download && go mod verify
COPY src/ ./
RUN go install github.com/google/wire/cmd/wire@latest \
 && wire ./cmd \
 && go build -o concurrent ./cmd

FROM golang:latest

COPY --from=builder /work/concurrent /usr/local/bin

CMD ["concurrent"]
