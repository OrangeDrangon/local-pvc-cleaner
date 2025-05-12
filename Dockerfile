FROM scratch
ENTRYPOINT ["/local-pvc-cleaner"]
COPY local-pvc-cleaner /local-pvc-cleaner