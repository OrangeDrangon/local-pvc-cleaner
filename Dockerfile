FROM scratch
ENTRYPOINT ["/usr/bin/local-pvc-cleaner"]
COPY local-pvc-cleaner /usr/bin