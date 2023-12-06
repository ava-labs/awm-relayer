ARG GO_VERSION

### Build Stage ###
FROM golang:${GO_VERSION}-bullseye as build

WORKDIR /go/src
# Copy the code into the container
COPY . .
RUN go mod tidy
# Build awm-relayer
RUN bash ./scripts/build.sh

### RUN Stage ###
FROM golang:${GO_VERSION}
COPY --from=build /go/src/build/awm-relayer /usr/bin/awm-relayer
EXPOSE 8080
USER 1001
CMD ["start"]
ENTRYPOINT ["/usr/bin/awm-relayer"]
