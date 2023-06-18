FROM golang:latest AS coreBuilder
WORKDIR /work

COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY ./ ./
RUN go install github.com/google/wire/cmd/wire@latest \
 && wire ./cmd \
 && go build -o concurrent ./cmd

FROM node:18 AS webBuilder
WORKDIR /work

RUN curl -f https://get.pnpm.io/v6.16.js | node - add --global pnpm
COPY ./web ./
RUN pnpm i && pnpm build

FROM golang:latest

COPY --from=coreBuilder /work/concurrent /usr/local/bin
COPY --from=webBuilder /work/dist /etc/www/concurrent

CMD ["concurrent"]
