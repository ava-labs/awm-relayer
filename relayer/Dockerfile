FROM debian:11-slim
RUN apt update && apt --yes install ca-certificates
COPY awm-relayer /usr/bin/awm-relayer
EXPOSE 8080
USER 1001
CMD ["start"]
ENTRYPOINT [ "/usr/bin/awm-relayer" ]
