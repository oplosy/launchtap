FROM node:24.16.0-alpine@sha256:21f403ab171f2dc89bad4dd69d7721bfd15f084ccb46cdd225f31f2bc59b5c9a AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1 CONTAINER_BUILD=1
COPY package.json package-lock.json ./
RUN npm install --global npm@12.0.1 && npm ci
COPY . .
ARG NEXT_PUBLIC_PRIVY_APP_ID
ARG NEXT_PUBLIC_DEPLOYMENT_ID
ARG NEXT_PUBLIC_CHAIN_ID
ARG NEXT_PUBLIC_API_BASE_URL
ARG NEXT_PUBLIC_RPC_URL
ARG NEXT_PUBLIC_WEB_ORIGIN
ENV NEXT_PUBLIC_PRIVY_APP_ID=$NEXT_PUBLIC_PRIVY_APP_ID \
    NEXT_PUBLIC_DEPLOYMENT_ID=$NEXT_PUBLIC_DEPLOYMENT_ID \
    NEXT_PUBLIC_CHAIN_ID=$NEXT_PUBLIC_CHAIN_ID \
    NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL \
    NEXT_PUBLIC_RPC_URL=$NEXT_PUBLIC_RPC_URL \
    NEXT_PUBLIC_WEB_ORIGIN=$NEXT_PUBLIC_WEB_ORIGIN \
    NEXT_PUBLIC_E2E_FIXTURE=0 NEXT_PUBLIC_LOCAL_DEV=0
RUN node scripts/container-preflight.mjs --record && npm run build

FROM node:24.16.0-alpine@sha256:21f403ab171f2dc89bad4dd69d7721bfd15f084ccb46cdd225f31f2bc59b5c9a AS runtime
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
# The production preflight reads the same generated manifest used by this build.
COPY --from=build /app/scripts/validate-release.mjs ./scripts/validate-release.mjs
COPY --from=build /app/scripts/container-preflight.mjs ./scripts/container-preflight.mjs
COPY --from=build /app/public-config.json ./public-config.json
COPY --from=build /app/src/contracts/generated.ts ./src/contracts/generated.ts
USER node
CMD ["node", "server.js"]
