FROM --platform=$BUILDPLATFORM quay.io/prometheus/busybox:latest
LABEL maintainer="Bo Peng <pengbo@sraoss.co.jp>"

ARG TARGETARCH

ENV POSTGRES_USERNAME postgres
ENV POSTGRES_PASSWORD postgres
ENV POSTGRES_DATABASE postgres
ENV PGPOOL_SERVICE localhost
ENV PGPOOL_SERVICE_PORT 9999
ENV SSLMODE disable

COPY pgpool2_exporter-$TARGETARCH /bin/pgpool2_exporter

CMD ["/bin/sh", "-c", "export DATA_SOURCE_USER=\"${POSTGRES_USERNAME}\" ; export DATA_SOURCE_PASS=\"${POSTGRES_PASSWORD}\" ; export DATA_SOURCE_URI=\"${PGPOOL_SERVICE}:${PGPOOL_SERVICE_PORT}/${POSTGRES_DATABASE}?sslmode=${SSLMODE}\" ; /bin/pgpool2_exporter"]

EXPOSE     9719
