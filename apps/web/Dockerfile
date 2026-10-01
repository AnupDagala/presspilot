FROM node:24.11.0-bookworm-slim AS build
WORKDIR /app
COPY package*.json ./
COPY apps/web/package.json apps/web/package.json
COPY apps/worker/package.json apps/worker/package.json
RUN npm ci
COPY apps/web apps/web
RUN npm run build --workspace=@presspilot/web
RUN npm prune --omit=dev
USER node
ENV PORT=8080 WEB_BIND=0.0.0.0
EXPOSE 8080
CMD ["node","apps/web/server.mjs"]
