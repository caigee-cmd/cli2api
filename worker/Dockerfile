FROM node:20-bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates git curl tini \
  && rm -rf /var/lib/apt/lists/* \
  && npm install -g @qoder-ai/qodercli@1.1.27

WORKDIR /app
COPY package.json ./
COPY src ./src
COPY last-plain.sample.json /data/last-plain.json

ENV NODE_ENV=production \
    HOME=/root \
    WORKER_HOST=0.0.0.0 \
    WORKER_PORT=3020 \
    QODERCLI_JS=/usr/local/lib/node_modules/@qoder-ai/qodercli/bundle/qodercli.js \
    PLAIN_TEMPLATE_PATH=/data/last-plain.json \
    QODER_WARMUP_CWD=/tmp

EXPOSE 3020
ENTRYPOINT ["/usr/bin/tini","--"]
CMD ["node","src/daemon.mjs"]
