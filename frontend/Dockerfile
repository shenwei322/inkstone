# 构建阶段：国内 npm 源 + 构建期注入 API 地址（NEXT_PUBLIC_* 只能构建期注入）
FROM docker.m.daocloud.io/library/node:22-alpine AS builder
WORKDIR /app
ARG NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
# 站点地址：canonical / OG / JSON-LD 使用。必须在这里声明 ARG 才会被
# docker build --build-arg 接收，漏了声明的话传入值被静默忽略、
# 仍用下面的默认值兜底。
ARG NEXT_PUBLIC_SITE_URL=http://localhost:3000
ENV NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL
RUN npm config set registry https://registry.npmmirror.com
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# 运行阶段：standalone 输出，仅复制必要产物（无需完整 node_modules）
FROM docker.m.daocloud.io/library/node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
EXPOSE 3000
CMD ["node", "server.js"]
