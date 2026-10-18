FROM node:22-alpine AS build
WORKDIR /workspace
COPY package.json package-lock.json ./
COPY apps/dashboard/package.json apps/dashboard/package.json
COPY apps/control-plane/package.json apps/control-plane/package.json
RUN npm ci --workspace=@scopra/control-plane --include-workspace-root=false --no-audit --no-fund
COPY apps/control-plane ./apps/control-plane
RUN npm run build --workspace=@scopra/control-plane

FROM node:22-alpine AS runtime
ENV NODE_ENV=production
WORKDIR /workspace
COPY package.json package-lock.json ./
COPY apps/dashboard/package.json apps/dashboard/package.json
COPY apps/control-plane/package.json apps/control-plane/package.json
RUN npm ci --omit=dev --workspace=@scopra/control-plane --include-workspace-root=false --no-audit --no-fund
COPY --from=build /workspace/apps/control-plane/dist ./apps/control-plane/dist
COPY --from=build /workspace/apps/control-plane/migrations ./apps/control-plane/migrations
USER node
EXPOSE 8080
CMD ["node", "apps/control-plane/dist/server.js"]
