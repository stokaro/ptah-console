# syntax=docker/dockerfile:1.27@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# The console's image: the binary `make build-linux` built, with the web app
# in it, on distroless as a non-root user. The build context holds the binary
# and this file and nothing else; `make image` makes that context.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE=https://github.com/stokaro/ptah-console
LABEL org.opencontainers.image.title="Ptah Console" \
      org.opencontainers.image.description="Web console for Ptah Operator" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$REVISION" \
      org.opencontainers.image.source="$SOURCE" \
      org.opencontainers.image.licenses="MIT"
COPY ptah-console /ptah-console
USER 65532:65532
ENTRYPOINT ["/ptah-console"]
