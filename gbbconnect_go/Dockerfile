# syntax=docker/dockerfile:1.7

ARG GBBCONNECT_IMAGE=ghcr.io/krzysztofhajdamowicz/gbbconnect-go:latest
ARG BUILD_ARCH=aarch64
FROM ${GBBCONNECT_IMAGE} AS gbbconnect

FROM ghcr.io/home-assistant/${BUILD_ARCH}-base:3.22

ARG BUILD_ARCH
ARG BUILD_VERSION=dev
LABEL io.hass.name="gbbconnect-go" \
      io.hass.description="Unofficial GbbOptimizer MQTT-to-Modbus bridge" \
      io.hass.type="app" \
      io.hass.version="${BUILD_VERSION}" \
      io.hass.arch="${BUILD_ARCH}"

COPY --from=gbbconnect /usr/local/bin/gbbconnect /usr/local/bin/gbbconnect
COPY render.jq /usr/share/gbbconnect/render.jq
COPY run.sh /run.sh
RUN chmod 0755 /run.sh /usr/local/bin/gbbconnect

CMD ["/run.sh"]
