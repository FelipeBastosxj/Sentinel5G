# =============================================================================
# Angular Dashboard — multi-stage Docker build
# =============================================================================
# Stage 1: build   — compiles with ng build --configuration production
# Stage 2: runtime — nginx serves the static bundle on port 4200
# =============================================================================

# ─── Stage 1: Build ───────────────────────────────────────────────────────────
FROM node:22-alpine AS build
WORKDIR /app

# Copy workspace manifests first (better layer caching)
COPY package.json package-lock.json ./
COPY shared/contracts/package.json  ./shared/contracts/
COPY frontend/angular-dashboard/package.json ./frontend/angular-dashboard/

# Install only the packages needed for the frontend + contracts
RUN npm install \
      --workspace=shared/contracts \
      --workspace=frontend/angular-dashboard \
      --ignore-scripts

# Copy source files
COPY tsconfig.base.json             ./
COPY shared/contracts/              ./shared/contracts/
COPY frontend/angular-dashboard/    ./frontend/angular-dashboard/

# Compile Angular for production
WORKDIR /app/frontend/angular-dashboard
RUN node ../../node_modules/@angular/cli/bin/ng.js build --configuration production

# ─── Stage 2: Runtime (nginx) ─────────────────────────────────────────────────
FROM nginx:1.27-alpine
COPY --from=build /app/frontend/angular-dashboard/dist/angular-dashboard/browser \
                  /usr/share/nginx/html
COPY infra/nginx/angular.conf /etc/nginx/conf.d/default.conf

EXPOSE 4200
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s \
  CMD wget -qO- http://localhost:4200/ || exit 1
