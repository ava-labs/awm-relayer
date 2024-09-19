FROM debian:12-slim
COPY signature-aggregator /usr/bin/signature-aggregator
RUN apt update && apt --yes install ca-certificates
EXPOSE 8080
EXPOSE 8081
CMD ["start"]
ENTRYPOINT [ "/usr/bin/signature-aggregator" ]
