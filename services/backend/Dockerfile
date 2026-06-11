FROM alpine:3.21

ARG PB_VERSION=0.38.2

ADD https://github.com/pocketbase/pocketbase/releases/download/v${PB_VERSION}/pocketbase_${PB_VERSION}_linux_amd64.zip /tmp/pb.zip
RUN apk add --no-cache unzip ca-certificates && \
    unzip /tmp/pb.zip -d /app/ && \
    apk del unzip && \
    rm /tmp/pb.zip && \
    addgroup -S -g 10001 backend && adduser -S -u 10001 -G backend backend

WORKDIR /app

COPY --chown=backend:backend ./pb_migrations ./pb_migrations
COPY --chown=backend:backend ./pb_hooks ./pb_hooks

EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8090/api/health

USER backend

CMD ["/bin/sh", "-c", "/app/pocketbase serve --http=0.0.0.0:8090 --origins=\"${PB_ORIGINS:-*}\""]
