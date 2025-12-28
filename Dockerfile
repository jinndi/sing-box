# https://github.com/SagerNet/sing-box/releases
ARG v_singbox="v1.12.14"

# https://hub.docker.com/_/alpine/tags
ARG v_alpine="3.23"

FROM ghcr.io/sagernet/sing-box:${v_singbox} AS sing-box

FROM alpine:${v_alpine}

LABEL org.opencontainers.image.version=${v_singbox}
LABEL org.opencontainers.image.title="sing-box"
LABEL org.opencontainers.image.description="sing-box docker image with routing rules"
LABEL org.opencontainers.image.documentation=https://github.com/jinndi/WGDashboard-sing-box
LABEL maintainer=Jinndi

ENV DATA_DIR=/data

COPY --from=sing-box \
      /usr/local/bin/sing-box \
      /bin/sing-box
COPY ./scripts/ /scripts/
COPY ./entrypoint.sh /entrypoint.sh
COPY ./sysctl.conf /etc/sysctl.conf

RUN set -ex && \
    apk add --no-cache \
      bash curl iptables iproute2 jq openssl idn2-utils  && \
    mkdir -p "$DATA_DIR" && chmod -R +x /scripts /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
