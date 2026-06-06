# ─── Dependencies ─────────────────────────────────────────────────────────────
FROM node:22-alpine AS deps
WORKDIR /app

COPY package.json package-lock.json ./
COPY shared/contracts/package.json ./shared/contracts/
COPY shared/utils/package.json      ./shared/utils/
COPY services/ingestion-service/package.json ./services/ingestion-service/

RUN npm install \
      --workspace=shared/contracts \
      --workspace=shared/utils \
      --workspace=services/ingestion-service \
      --include=dev \
      --ignore-scripts && \
    npm install --ignore-scripts zod @nestjs/throttler @nestjs/config

# ─── Build ────────────────────────────────────────────────────────────────────
FROM node:22-alpine AS build
WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules

COPY tsconfig.base.json ./
COPY shared/contracts/ ./shared/contracts/
COPY shared/utils/      ./shared/utils/
COPY services/ingestion-service/ ./services/ingestion-service/

WORKDIR /app/shared/contracts
RUN npx tsc -p tsconfig.json

WORKDIR /app/shared/utils
RUN npx tsc -p tsconfig.build.json

WORKDIR /app/services/ingestion-service
RUN npx tsc -p tsconfig.json && \
    MAIN=$(find dist -name 'main.js' -type f | head -1) && \
    test -n "$MAIN" || (echo "❌ main.js not found in dist/" && exit 1) && \
    echo "✅ main.js → $MAIN"

# ─── Runtime ──────────────────────────────────────────────────────────────────
FROM node:22-alpine
WORKDIR /app

COPY --from=build /app/node_modules               ./node_modules
COPY --from=build /app/shared/contracts            ./shared/contracts
COPY --from=build /app/shared/utils                ./shared/utils
COPY --from=build /app/services/ingestion-service  ./services/ingestion-service

WORKDIR /app/services/ingestion-service

ENV NODE_ENV=production
EXPOSE 3001
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s \
  CMD wget -qO- http://localhost:3001/health || exit 1

CMD ["node", "dist/services/ingestion-service/src/main.js"]
