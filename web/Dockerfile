FROM node:18 AS webBuilder
WORKDIR /work

COPY /web ./
RUN npm i -g pnpm \
 && pnpm i && pnpm build

FROM nginx
COPY ./web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=webBuilder /work/dist /usr/share/nginx/html

