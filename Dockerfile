FROM quay.io/prometheus/busybox:latest

COPY pgpool2_exporter /bin/pgpool2_exporter

ENTRYPOINT ["/bin/pgpool2_exporter"]
EXPOSE     9719
