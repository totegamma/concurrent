FROM node:18 AS webBuilder
WORKDIR /work

RUN curl -f https://get.pnpm.io/v6.16.js | node - add --global pnpm
COPY /web ./
RUN pnpm i && pnpm build

FROM nginx
COPY ./web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=webBuilder /work/dist /usr/share/nginx/html

