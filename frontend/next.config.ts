import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  images: {
    // 封面图由后端 /uploads 提供（开发环境为 localhost:8080）。
    // next/image 会经优化代理加载这些远端图片；生产若改用绝对域名，需在此追加对应 hostname。
    remotePatterns: [
      { protocol: "http", hostname: "localhost", port: "8080", pathname: "/uploads/**" },
      { protocol: "http", hostname: "127.0.0.1", port: "8080", pathname: "/uploads/**" },
      // 生产：封面走绝对地址 https://blog.shenv.top/uploads/...（需后端 PUBLIC_API_URL 设为该域名）
      { protocol: "https", hostname: "blog.shenv.top", pathname: "/uploads/**" },
    ],
    // 开发环境 localhost 会解析到私有 IP，被 Next 的 SSRF 防护拦截，需显式放行（仅开发需要）。
    dangerouslyAllowLocalIP: true,
  },
  // 对 barrel 导出库做 tree-shaking（lucide-react 全站图标），减小产物体积。
  experimental: {
    optimizePackageImports: ["lucide-react"],
  },
  // standalone 输出：运行镜像只含必要依赖（785MB → ~200MB），部署镜像体积大幅缩小。
  output: "standalone",
  // Next 16 dev 默认拦截非 localhost 源的静态资源（HTML 200 但 CSS/JS chunk 403 → 页面无 UI）。
  // 用 127.0.0.1 访问 dev 时需显式放行（仅开发生效；生产不受影响）。
  allowedDevOrigins: ["localhost", "127.0.0.1"],
};

export default nextConfig;
