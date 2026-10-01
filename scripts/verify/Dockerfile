# Verification-only image for the isolated checks in scripts/verify/. The
# demo does not depend on Docker. Base images are pinned by digest.
#
# The build context is only this file plus the tested commit's go.mod and
# go.sum (build-image.sh copies them from git), never the working tree or
# .env. The preparation phase downloads exactly those modules, checked against
# go.sum and sum.golang.org, into a read-only cache; the test phase then runs
# with no network.
FROM node:22.22.2-bookworm-slim@sha256:9f6d5975c7dca860947d3915877f85607946403fc55349f39b4bc3688448bb6e AS node

# Go 1.26.4 on Debian 12, which already ships gcc, make, git, curl, procps
# (pgrep) and python3 3.11.2.
FROM golang:1.26.4-bookworm@sha256:b305420a68d0f229d91eb3b3ed9e519fcf2cf5461da4bef997bf927e8c0bfd2b
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY go.mod go.sum /opt/apa/deps/
# The hashes are taken before the download, and the build fails if the
# download changed either file: `go mod download all` would add go.sum lines
# for modules outside this build, so only go.mod's requirements are fetched.
# The second download runs with GOPROXY=off to prove the cache serves every
# requirement offline. (go list -m all would also need go.mod files of
# graph-only modules that this build never reads.)
RUN cd /opt/apa/deps \
 && sha256sum go.mod go.sum >/opt/apa/deps.sha256 \
 && GOMODCACHE=/opt/apa/gomodcache GOTOOLCHAIN=local GOFLAGS=-mod=readonly go mod download \
 && sha256sum -c /opt/apa/deps.sha256 \
 && GOMODCACHE=/opt/apa/gomodcache GOTOOLCHAIN=local GOFLAGS=-mod=readonly GOPROXY=off go mod download \
 && chmod -R a+rX,a-w /opt/apa
LABEL org.opencontainers.image.title="apa-verify" \
      org.opencontainers.image.description="Isolated verification of agent-payment-authority; not a runtime image"
