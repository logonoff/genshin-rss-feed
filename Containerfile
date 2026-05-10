FROM golang:1-alpine AS builder

WORKDIR /build
COPY . .
RUN apk add --no-cache make
RUN make build

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/bin/server /server
USER 65534
EXPOSE 3000
CMD ["/server"]
