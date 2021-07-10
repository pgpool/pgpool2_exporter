FROM quay.io/prometheus/busybox:latest
LABEL maintainer="Bo Peng <pengbo@sraoss.co.jp>"

ENV POSTGRES_USERNAME postgres
ENV POSTGRES_PASSWORD postgres
ENV PGPOOL_SERVICE localhost
ENV PGPOOL_SERVICE_PORT 9999


COPY pgpool2_exporter /bin/pgpool2_exporter


CMD ["/bin/sh", "-c", "export DATA_SOURCE_NAME=\"postgresql://${POSTGRES_USERNAME}:${POSTGRES_PASSWORD}@${PGPOOL_SERVICE}:${PGPOOL_SERVICE_PORT}/postgres?sslmode=disable\" ; /bin/pgpool2_exporter"]

EXPOSE     9719
