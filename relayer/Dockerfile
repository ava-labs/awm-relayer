FROM debian:12-slim
RUN apt update && apt --yes install ca-certificates
COPY icm-relayer /usr/bin/icm-relayer
EXPOSE 8080
USER 1001
CMD ["start"]
ENTRYPOINT [ "/usr/bin/icm-relayer" ]
