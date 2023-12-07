### Build Stage ###
ARG GO_VERSION
FROM golang:${GO_VERSION}-bullseye as build

WORKDIR /go/src
# Copy the code into the container
COPY . .
RUN go mod tidy
# Build awm-relayer
RUN bash ./scripts/build.sh

### RUN Stage ###
ARG GO_VERSION
FROM golang:${GO_VERSION}
COPY --from=build /go/src/build/awm-relayer /usr/bin/awm-relayer
EXPOSE 8080
USER 1001
CMD ["start"]
ENTRYPOINT ["/usr/bin/awm-relayer"]
