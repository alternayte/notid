# The server bundle is plain JavaScript, so it builds once on the runner's platform for every target.
FROM --platform=$BUILDPLATFORM oven/bun:1.4.3 AS build
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY . .
RUN bun run build

FROM oven/bun:1.4.3-slim
WORKDIR /app
COPY --from=build /app/.output ./.output
ENV NODE_ENV=production PORT=3000 HOST=0.0.0.0
USER bun
EXPOSE 3000
CMD ["bun", ".output/server/index.mjs"]
