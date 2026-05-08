# ── Build stage ───────────────────────────────────────────────────────────────
FROM node:20-alpine AS builder

WORKDIR /app

# Copy lockfile first for layer caching.
# `npm ci` is used (not `npm install`): it's faster, reproducible,
# and will fail if package-lock.json is out of sync with package.json.
COPY package.json package-lock.json ./
RUN npm ci

COPY . .

# Build the production React app. Vite outputs to /app/dist.
# The PWA service worker and manifest are generated here.
RUN npm run build

# ── Runtime stage ─────────────────────────────────────────────────────────────
# Nginx serves the static React build and proxies /api/* to the backend.
FROM nginx:1.25-alpine

# Remove default Nginx config
RUN rm /etc/nginx/conf.d/default.conf

COPY nginx.conf /etc/nginx/conf.d/app.conf
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
