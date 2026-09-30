# Verification-only image for the isolated B1-02 checks in scripts/verify/.
# The demo does not depend on Docker. Base images are pinned by digest; no
# package is installed at build time.
FROM node:22.22.2-bookworm-slim@sha256:9f6d5975c7dca860947d3915877f85607946403fc55349f39b4bc3688448bb6e AS node

# Go 1.26.4 on Debian 12, which already ships gcc, make, git, curl, procps
# (pgrep) and python3 3.11.2.
FROM golang:1.26.4-bookworm@sha256:b305420a68d0f229d91eb3b3ed9e519fcf2cf5461da4bef997bf927e8c0bfd2b
COPY --from=node /usr/local/bin/node /usr/local/bin/node
LABEL org.opencontainers.image.title="apa-verify" \
      org.opencontainers.image.description="Isolated verification of agent-payment-authority contracts; not a runtime image"
