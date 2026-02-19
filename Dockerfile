# syntax=docker/dockerfile:1.2
FROM alpine:3.23

RUN apk add --no-cache --no-progress ca-certificates tzdata

ARG TARGETPLATFORM
COPY ./dist/$TARGETPLATFORM/traefik /
COPY ./traefik.sample.toml /

EXPOSE 80
VOLUME ["/tmp"]

ENTRYPOINT ["/traefik"]
CMD ["--configFile=traefik.sample.toml"]