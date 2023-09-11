FROM golang:latest AS coreBuilder
WORKDIR /work

RUN go install github.com/google/wire/cmd/wire@latest
COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY ./ ./
RUN wire ./cmd/api \
 && go build -o ccapi ./cmd/api

FROM golang:latest

# RUN go install github.com/totegamma/ccadmin
COPY --from=coreBuilder /work/ccapi /usr/local/bin

CMD ["ccapi"]
