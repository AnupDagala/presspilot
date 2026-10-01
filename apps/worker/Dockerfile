FROM node:24.11.0-bookworm-slim AS build
WORKDIR /app
COPY package*.json ./
COPY apps/web/package.json apps/web/package.json
COPY apps/worker/package.json apps/worker/package.json
RUN npm ci
COPY apps/worker apps/worker
RUN npm run build --workspace=@presspilot/worker
RUN npm prune --omit=dev
USER node
ENV WORKER_HEALTH_BIND=0.0.0.0
CMD ["node","apps/worker/dist/worker.js"]
