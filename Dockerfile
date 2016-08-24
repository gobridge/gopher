FROM alpine:3.4
MAINTAINER Florin Patan "florinpatan@gmail.com"

EXPOSE 8080
WORKDIR /

# DNS stuff
RUN echo 'hosts: files mdns4_minimal [NOTFOUND=return] dns mdns4' >> /etc/nsswitch.conf

# SSL certs
RUN apk add --update ca-certificates \
    && rm -rf /var/cache/apk/*

# Binary
ADD gopher /gopher

# Runtime
CMD ["/gopher"]
