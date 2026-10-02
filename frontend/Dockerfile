# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# Site key Turnstile bersifat publik dan "ditanam" saat build (React env build-time)
ARG REACT_APP_TURNSTILE_SITE_KEY=""
ENV REACT_APP_TURNSTILE_SITE_KEY=$REACT_APP_TURNSTILE_SITE_KEY

# Install dependensi sesuai lockfile
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

# Copy source code
COPY public ./public
COPY src ./src
COPY tailwind.config.js postcss.config.js ./

# Build the app
RUN npm run build

# Production stage
FROM nginx:alpine

# Copy built assets from builder
COPY --from=builder /app/build /usr/share/nginx/html

# Copy nginx config
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Port 3000 agar sama dengan target Nginx Proxy Manager (mihanstore_frontend:3000)
EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]
