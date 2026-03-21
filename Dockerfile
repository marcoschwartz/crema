FROM scratch
COPY ca-certificates.crt /etc/ssl/certs/
COPY crema-bin /crema
ENTRYPOINT ["/crema"]
