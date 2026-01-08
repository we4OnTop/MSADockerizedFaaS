FROM openresty/openresty:alpine

RUN apk add --no-cache perl curl

RUN opm get ledgetech/lua-resty-http

COPY ./nginx/nginx.conf /usr/local/openresty/nginx/conf/nginx.conf

CMD ["/usr/local/openresty/bin/openresty", "-g", "daemon off;"]