FROM golang:latest AS coreBuilder
WORKDIR /work

RUN go install github.com/google/wire/cmd/wire@latest
COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY ./ ./
RUN wire ./cmd/api \
 && go build -o ccapi ./cmd/api

FROM node:18 AS webBuilder
WORKDIR /work

RUN curl -f https://get.pnpm.io/v6.16.js | node - add --global pnpm
COPY /web ./
RUN pnpm i && pnpm build

FROM golang:latest

COPY --from=coreBuilder /work/ccapi /usr/local/bin
COPY --from=webBuilder /work/dist /etc/www/concurrent

CMD ["ccapi"]
