FROM alpine:latest

ARG PB_VERSION=0.36.9

RUN apk add --no-cache \
    unzip \
    ca-certificates

ADD https://github.com/pocketbase/pocketbase/releases/download/v${PB_VERSION}/pocketbase_${PB_VERSION}_linux_amd64.zip /tmp/pb.zip
RUN unzip /tmp/pb.zip -d /app/

COPY ./pb_migrations /pb/pb_migrations

COPY ./pb_hooks /pb/pb_hooks

EXPOSE 8090

RUN /app/pocketbase superuser create "${PB_SUPERUSER_EMAIL}" "${PB_SUPERUSER_PASSWORD}"

# start PocketBase
CMD /app/pocketbase serve --http=0.0.0.0:8090 --origins="${PB_ORIGINS:-*}"