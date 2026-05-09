FROM golang:1-alpine3.23 AS builder

WORKDIR /build
COPY . .
RUN apk add --no-cache make
RUN make build

FROM alpine:3.23

COPY --from=builder /build/bin/server /server
USER 65534
EXPOSE 3000
CMD ["/server"]
