# syntax=docker/dockerfile:1.27@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# The console's image: the binary already built for the target platform,
# with the web app in it, on distroless as a non-root user. The build context
# holds this file and <os>/<arch>/ptah-console for each platform and nothing
# else; GoReleaser makes that context for a release, and `make image` for one
# platform.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG TARGETPLATFORM
ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE=https://github.com/stokaro/ptah-console
LABEL org.opencontainers.image.title="Ptah Console" \
      org.opencontainers.image.description="Web console for Ptah Operator" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$REVISION" \
      org.opencontainers.image.source="$SOURCE" \
      org.opencontainers.image.licenses="MIT"
# The mode is set here rather than taken from the context, which carries the
# umask of whoever built the binary: a binary only its owner may run is one
# the non-root user cannot start.
COPY --chmod=0555 $TARGETPLATFORM/ptah-console /ptah-console
USER 65532:65532
ENTRYPOINT ["/ptah-console"]
