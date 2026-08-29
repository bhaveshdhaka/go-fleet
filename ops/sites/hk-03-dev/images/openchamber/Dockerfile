# syntax=docker/dockerfile:1
# openchamber overlay (hk-03-dev): the repo Dockerfile builds the app
# (BASE_IMAGE); this adds the global `openchamber` CLI the registry args
# expect (openchamber serve --foreground) and the chromium libs the
# registry parity check requires. Committed intent — the pre-wipe copy
# lived untracked on the old box and was lost with it.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

USER root
# /root must be traversable by uid 1000: the cluster kubeconfig mounts at
# /root/.kube/fleet.kubeconfig (same absolute path on host and pod) and
# fleet ops resolve it from pod sessions too.
RUN chmod 755 /root
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl libglib2.0-0 libnss3 libnspr4 libdbus-1-3 libatk1.0-0 libatk-bridge2.0-0 \
    libcups2 libdrm2 libxkbcommon0 libatspi2.0-0 libxcomposite1 libxdamage1 \
    libxfixes3 libxrandr2 libgbm1 libpango-1.0-0 libcairo2 libasound2 \
    libx11-6 libxext6 \
    && rm -rf /var/lib/apt/lists/*
# kubectl pinned to TOOLCHAIN_KUBECTL_VERSION — fleet ops shells out to it;
# pod sessions get the same binary the host toolchain pins.
RUN curl -fsSL -o /usr/local/bin/kubectl \
      https://dl.k8s.io/release/v1.36.3/bin/linux/amd64/kubectl \
    && chmod +x /usr/local/bin/kubectl && kubectl version --client=true

# System-level CLI install: the oc-tools PVC mounts OVER
# /home/openchamber/.npm-global, so a user-prefix install is shadowed on
# a fresh volume — /usr/local/bin is outside every mount.
USER root
RUN npm config set prefix /usr/local \
    && npm install -g /home/openchamber/packages/web \
    && openchamber --version
