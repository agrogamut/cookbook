# Build and run the MadamGY API.
#
# The runtime stage carries a browser and Bengali fonts, which is unusual for a Go service and
# is the point: every ingredient carries a Bengali name, Bengali needs conjunct formation and
# matra repositioning that no Go PDF library shapes, and a renderer that mis-shapes an
# ingredient name is worse than one that is absent. No browser means /books/*.pdf returns 503
# rather than printing something subtly wrong.

FROM golang:1.26-bookworm AS build
WORKDIR /src

# Dependencies first, so a source-only change does not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

# CGO off so the binaries do not need the toolchain's shared libraries at runtime.
ENV CGO_ENABLED=0
RUN go build -o /out/server ./cmd/server && \
    go build -o /out/import ./cmd/import

FROM debian:bookworm-slim
WORKDIR /app

# fonts-noto-core carries Bengali. Without it Chromium has no glyphs for the ingredient names
# and prints boxes -- which looks like a rendering bug and is really a missing font, so it is
# installed explicitly rather than being inherited from whatever the base image happens to
# ship. fonts-noto-color-emoji is deliberately not installed: nothing in these books uses one.
RUN apt-get update && apt-get install -y --no-install-recommends \
        chromium \
        fonts-noto-core \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/server /out/import /usr/local/bin/

# The provider's 14 workbooks are the source of truth and are committed to the repository, so
# the importer reads them from the image rather than needing a volume or a download.
COPY data/provider/ /app/data/provider/

COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV XLSX_DIR=/app/data/provider \
    CHROMIUM_NO_SANDBOX=1

EXPOSE 8080
ENTRYPOINT ["docker-entrypoint.sh"]
