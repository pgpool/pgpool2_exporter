FROM golang:alpine AS builder
RUN mkdir /pgpool2_exporter
COPY . /pgpool2_exporter
WORKDIR /pgpool2_exporter
RUN CGO_ENABLED=0 go build -o pgpool2_exporter cmd/pgpool2_exporter/main.go

FROM alpine
WORKDIR /api-server
COPY --from=builder /pgpool2_exporter/ /bin/pgpool2_exporter/

CMD ["/bin/sh", "-c", "export DATA_SOURCE_USER=\"${POSTGRES_USERNAME}\" ; export DATA_SOURCE_PASS=\"${POSTGRES_PASSWORD}\" ; export DATA_SOURCE_URI=\"${PGPOOL_SERVICE}:${PGPOOL_SERVICE_PORT}/${POSTGRES_DATABASE}?sslmode=${SSLMODE}\" ; /bin/pgpool2_exporter/pgpool2_exporter"]

EXPOSE 9719
