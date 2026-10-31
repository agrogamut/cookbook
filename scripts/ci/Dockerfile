ARG BUILD_IMAGE
ARG TEST_RUNTIME
FROM ${BUILD_IMAGE} AS test-toolchain
FROM ${TEST_RUNTIME}
COPY --from=test-toolchain /usr/local/go /toolchain
ENV GOROOT=/toolchain PATH=/toolchain/bin:/usr/local/bin:/usr/bin:/bin GOTOOLCHAIN=local
RUN set -eu; \
    . /usr/local/share/pdf-runtime.lock; \
    test "$(go version)" = "go version go${GO_VERSION} linux/amd64"; \
    apt-get update; \
    (cd /var/lib/apt/lists && printf '%s\n' "$APT_RELEASE_SHA256" | sha256sum -c -); \
    apt-get install -y --no-install-recommends $CI_FISH_PACKAGES git=1:2.39.5-0+deb12u3 python3=3.11.2-1+b1; \
    printf '%s\n' "$DEBIAN_PACKAGE_SET" | while IFS="$(printf '\t')" read -r package version; do \
        test "$(dpkg-query -W -f='${Version}' "$package")" = "$version"; \
    done; \
    dpkg-query -W -f='${binary:Package}\t${Version}\n' | LC_ALL=C sort > /usr/local/share/ci-packages.tsv; \
    printf '%s\n' "$SYSTEM_FONT_SHA256" | sha256sum -c -; \
    test "$(chromium --version)" = "$CHROMIUM_VERSION"; \
    command -v fish; command -v python3; command -v git; \
    rm -rf /var/lib/apt/lists/*
ENTRYPOINT []
