# c-5 fixture — a Dockerfile with planted issues the agnostic fallback is blind to.
# This file makes the `docker` marker profile surface (hadolint + trivy config +
# dockle). dockle (container image-layer CIS) needs a BUILT image, which dross never
# builds — so on this source-only fixture dockle is skipped-with-reason, while its
# EXPECTED image-layer findings are pinned in expected-finding.txt. See RUN.md.
#
# Planted defects:
#   - unpinned :latest base image      (hadolint DL3007 / dockle DKL-DI-0006)
#   - apt-get without cleanup          (hadolint DL3009)
#   - no USER (image runs as root)     (dockle CIS-DI-0001)
FROM ubuntu:latest
RUN apt-get update && apt-get install -y curl
COPY app /app
CMD ["/app"]
