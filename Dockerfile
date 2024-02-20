ARG GO_VERSION
FROM golang:${GO_VERSION}
COPY awm-relayer /usr/bin/awm-relayer
EXPOSE 8080
USER 1001
CMD ["start"]
ENTRYPOINT [ "/usr/bin/awm-relayer" ]