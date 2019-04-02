FROM golang:1.12.1 as builder
LABEL maintainer="GoBridge <support@gobridge.org>"

WORKDIR /code

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor .

FROM alpine:latest

WORKDIR /root

# DNS stuff
RUN echo 'hosts: files mdns4_minimal [NOTFOUND=return] dns mdns4' >> /etc/nsswitch.conf

# SSL certs
RUN apk add --update ca-certificates \
  && rm -rf /var/cache/apk/*

COPY --from=builder /code/gopher .

EXPOSE 8081

CMD ["./gopher"]
