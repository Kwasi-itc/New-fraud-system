FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22-alpine AS builder
WORKDIR /app
ARG NEXT_PUBLIC_DATA_MODEL_SERVICE_URL=
ARG NEXT_PUBLIC_INGESTION_SERVICE_URL=
ARG NEXT_PUBLIC_DECISION_ENGINE_SERVICE_URL=
ARG NEXT_PUBLIC_DATA_MODEL_TENANT_ID=
ENV NEXT_PUBLIC_DATA_MODEL_SERVICE_URL=$NEXT_PUBLIC_DATA_MODEL_SERVICE_URL
ENV NEXT_PUBLIC_INGESTION_SERVICE_URL=$NEXT_PUBLIC_INGESTION_SERVICE_URL
ENV NEXT_PUBLIC_DECISION_ENGINE_SERVICE_URL=$NEXT_PUBLIC_DECISION_ENGINE_SERVICE_URL
ENV NEXT_PUBLIC_DATA_MODEL_TENANT_ID=$NEXT_PUBLIC_DATA_MODEL_TENANT_ID
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV CASE_MANAGER_SERVICE_URL=http://case-manager-service:8086
ENV DECISION_ENGINE_SERVICE_URL=http://decision-engine-service:8082
ENV SCREENING_SERVICE_URL=http://screening-service:8085
ENV NEXT_PUBLIC_DATA_MODEL_SERVICE_URL=
ENV NEXT_PUBLIC_INGESTION_SERVICE_URL=
ENV NEXT_PUBLIC_DECISION_ENGINE_SERVICE_URL=
ENV NEXT_PUBLIC_DATA_MODEL_TENANT_ID=
COPY --from=builder --chown=node:node /app/.next/standalone ./
COPY --from=builder --chown=node:node /app/.next/static ./.next/static
COPY --from=builder --chown=node:node /app/public ./public
USER node

EXPOSE 3000

HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=12 \
  CMD node -e "fetch('http://127.0.0.1:3000/login').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"

CMD ["node", "server.js"]
