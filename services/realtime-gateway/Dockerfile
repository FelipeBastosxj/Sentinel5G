# ─── Dependencies ─────────────────────────────────────────────────────────────
FROM node:22-alpine AS deps
WORKDIR /app

COPY package.json package-lock.json ./
COPY shared/contracts/package.json ./shared/contracts/
COPY shared/utils/package.json      ./shared/utils/
COPY shared/schemas/package.json    ./shared/schemas/
COPY services/realtime-gateway/package.json ./services/realtime-gateway/

RUN npm install \
      --workspace=shared/contracts \
      --workspace=shared/utils \
      --workspace=shared/schemas \
      --workspace=services/realtime-gateway \
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
COPY shared/schemas/    ./shared/schemas/
COPY services/realtime-gateway/ ./services/realtime-gateway/

# ─── Build shared packages in dependency order ────────────────────────────────
WORKDIR /app/shared/contracts
RUN npx tsc -p tsconfig.json

WORKDIR /app/shared/utils
RUN npx tsc -p tsconfig.build.json

WORKDIR /app/shared/schemas
RUN npx tsc -p tsconfig.build.json

WORKDIR /app/services/realtime-gateway
RUN npx tsc -p tsconfig.json && \
    MAIN=$(find dist -name 'main.js' -type f | head -1) && \
    test -n "$MAIN" || (echo "❌ main.js not found anywhere in dist/" && exit 1) && \
    echo "✅ main.js → $MAIN"

# ─── Runtime ──────────────────────────────────────────────────────────────────
FROM node:22-alpine
WORKDIR /app

COPY --from=build /app/node_modules              ./node_modules
COPY --from=build /app/shared/contracts           ./shared/contracts
COPY --from=build /app/shared/utils               ./shared/utils
COPY --from=build /app/shared/schemas             ./shared/schemas
COPY --from=build /app/services/realtime-gateway  ./services/realtime-gateway

WORKDIR /app/services/realtime-gateway

ENV NODE_ENV=production
EXPOSE 3004
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s \
  CMD wget -qO- http://localhost:3004/health || exit 1

CMD ["node", "dist/services/realtime-gateway/src/main.js"]
