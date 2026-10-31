# Build and run the MadamGY API.
#
# The runtime stage carries a browser and Bengali fonts, which is unusual for a Go service and
# is the point: every ingredient carries a Bengali name, Bengali needs conjunct formation and
# matra repositioning that no Go PDF library shapes, and a renderer that mis-shapes an
# ingredient name is worse than one that is absent. No browser means /books/*.pdf returns 503
# rather than printing something subtly wrong.

FROM --platform=linux/amd64 golang:1.26.10-bookworm@sha256:2bba1f6b063d0e09c5297fbd0a7b9664c4931afc4476a779d7c9a3062704f949 AS build
ENV GOTOOLCHAIN=local
WORKDIR /src

# Dependencies first, so a source-only change does not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

# CGO off so the binaries do not need the toolchain's shared libraries at runtime.
ENV CGO_ENABLED=0
RUN go build -o /out/server ./cmd/server && \
    go build -o /out/import ./cmd/import && \
    go build -o /out/import-cases ./cmd/import-cases && \
    go build -o /out/staff-admin ./cmd/staff-admin && \
    go clean -cache

FROM --platform=linux/amd64 debian:bookworm-slim@sha256:a3bba7dc7dbcc1f37c2856c51fc9a7fac96608f50cb87b1b4e4d8b8c85ae988e
WORKDIR /app

# The pinned builder supplies trusted certificates before the runtime installs its own.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY docker/pdf-runtime.lock /usr/local/share/pdf-runtime.lock
RUN set -eu; \
    . /usr/local/share/pdf-runtime.lock; \
    test "$(dpkg --print-architecture)" = amd64; \
    rm -f /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources; \
    printf '%s\n' "$APT_SOURCES" > /etc/apt/sources.list.d/pdf-runtime.list; \
    apt-get update; \
    (cd /var/lib/apt/lists && printf '%s\n' "$APT_RELEASE_SHA256" | sha256sum -c -); \
    apt-get install -y --no-install-recommends $APT_PACKAGES; \
    dpkg-query -W -f='${binary:Package}\t${Version}\n' > /tmp/pdf-runtime.packages; \
    LC_ALL=C sort /tmp/pdf-runtime.packages > /tmp/pdf-runtime.sorted; \
    printf '%s\n' "$DEBIAN_PACKAGE_SET" > /tmp/pdf-runtime.expected; \
    diff -u /tmp/pdf-runtime.expected /tmp/pdf-runtime.sorted; \
    printf '%s  %s\n' "$DEBIAN_PACKAGE_SET_SHA256" /tmp/pdf-runtime.sorted | sha256sum -c -; \
    printf '%s\n' "$SYSTEM_FONT_SHA256" | sha256sum -c -; \
    test "$(chromium --version)" = "$CHROMIUM_VERSION"; \
    test "$(fc-match -f "%{family}" "Noto Sans Bengali")" = "Noto Sans Bengali"; \
    test "$(fc-match -f "%{family}" "Noto Serif Bengali")" = "Noto Serif Bengali"; \
    rm -rf /var/lib/apt/lists/* /tmp/pdf-runtime.packages /tmp/pdf-runtime.sorted /tmp/pdf-runtime.expected

# Debian's Writer 7.4 drops the supplied Bengali reference cell's tail and upload.
RUN set -eu; \
    . /usr/local/share/pdf-runtime.lock; \
    test "$(dpkg --print-architecture)" = amd64; \
    curl --fail --silent --show-error --max-time 180 --proto '=https' \
        "$WRITER_URL" \
        -o /tmp/libreoffice.tar.gz; \
    printf '%s  %s\n' "$WRITER_SHA256" /tmp/libreoffice.tar.gz | sha256sum -c -; \
    mkdir /tmp/libreoffice; \
    tar -xzf /tmp/libreoffice.tar.gz -C /tmp/libreoffice; \
    tdf_dir=/tmp/libreoffice/LibreOffice_26.2.1.2_Linux_x86-64_deb/DEBS; \
    set --; \
    for tdf_package in libreoffice26.2-ure libobasis26.2-core libobasis26.2-images \
        libobasis26.2-ooofonts libobasis26.2-writer libobasis26.2-en-us \
        libreoffice26.2 libreoffice26.2-writer libreoffice26.2-en-us; do \
        set -- "$@" "$tdf_dir/${tdf_package}_26.2.1.2-2_amd64.deb"; \
    done; \
    dpkg -i "$@"; \
    rm -rf /tmp/libreoffice /tmp/libreoffice.tar.gz /opt/libreoffice26.2/share/fonts; \
    ln -s /opt/libreoffice26.2/program/soffice /usr/local/bin/libreoffice; \
    test "$(command -v libreoffice)" = /usr/local/bin/libreoffice

# Native conversion uses the same hashed font bytes as the embedded renderer.
COPY internal/bookdocx/fonts/ /usr/share/fonts/bookdocx/
RUN . /usr/local/share/pdf-runtime.lock && cd /usr/share/fonts/bookdocx && sha256sum -c SHA256SUMS \
    && test "$(fc-scan --format '%{family}' NotoSans-Regular.ttf)" = "Noto Sans" \
    && test "$(fc-scan --format '%{family}' NotoSansBengali-Regular.ttf)" = "Noto Sans Bengali" \
    && test "$(fc-scan --format '%{family}' NotoSansDevanagari-Regular.ttf)" = "Noto Sans Devanagari" \
    && test "$(libreoffice --headless --version)" = "$WRITER_VERSION" \
    && command -v pdffonts && command -v prlimit

# staff-admin creates the first administrator. It ships in the image because there is no
# other way to run it against a deployed database: staff accounts are provisioned, never
# self-registered, so without this binary a fresh deployment has nobody who can sign in and
# no route to creating them. It is a one-off job's start command, never the entrypoint.
COPY --from=build /out/server /out/import /out/import-cases /out/staff-admin /usr/local/bin/

# The provider's 14 workbooks are the source of truth and are committed to the repository, so
# the importer reads them from the image rather than needing a volume or a download.
COPY data/provider/ /app/data/provider/
COPY data/case-source/ /app/data/case-source/

COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV XLSX_DIR=/app/data/provider \
    CHROMIUM_NO_SANDBOX=1

EXPOSE 8080
ENTRYPOINT ["docker-entrypoint.sh"]
