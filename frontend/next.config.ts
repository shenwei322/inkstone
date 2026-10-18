import type { NextConfig } from "next";

// 后端源站：从 NEXT_PUBLIC_API_URL 去掉末尾的 /api/v1 得到。
// rewrites 与 images.remotePatterns 共用这一份推导——换域名只改环境变量，
// 不用再回来改代码（曾经因为这里写死 hostname，换部署地址后图片全部报
// "hostname ... is not configured under images"）。
const apiBase = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1";
const backendOrigin = apiBase.replace(/\/api\/v1\/?$/, "");

// 后端有两处静态资源目录，两者都要放行，少一个就会报上面那个错：
//   /uploads  —— 编辑器上传的图片（uploads_handler.go）
//   /files    —— 文件管理里的附件（file_handler.go，URL 形如 /files/<StoredName>）
const STATIC_PATHS = ["/uploads/**", "/files/**"] as const;

// 开发环境固定放行本机两种写法（localhost 与 127.0.0.1 在浏览器里是两个源），
// 再并入由环境变量推导出的后端源站，避免生产域名漏配。
const imageHosts = new Set<string>([
  "http://localhost:8080",
  "http://127.0.0.1:8080",
]);
for (const origin of [backendOrigin, process.env.PUBLIC_API_URL]) {
  if (!origin) continue;
  try {
    const url = new URL(origin);
    imageHosts.add(`${url.protocol}//${url.host}`);
  } catch {
    // 环境变量填了非法地址时忽略，其余来源照常生效
  }
}

const remotePatterns = [...imageHosts].flatMap((origin) => {
  const url = new URL(origin);
  return STATIC_PATHS.map((pathname) => ({
    protocol: url.protocol.replace(":", "") as "http" | "https",
    hostname: url.hostname,
    // 端口为空时必须省略该字段，传空字符串会让 Next 校验失败
    ...(url.port ? { port: url.port } : {}),
    pathname,
  }));
});

const nextConfig: NextConfig = {
  // RSS 订阅源由后端 gin 根路由提供（/feed.xml）。
  // 生产环境由 Nginx 的 `location = /feed.xml` 精确匹配直接转发到后端，
  // 请求根本到不了前端；这条 rewrite 只服务于「没有 Nginx」的场景
  // （本地 dev、直接跑 next start、二次部署），否则页脚的订阅链接会 404。
  async rewrites() {
    return [{ source: "/feed.xml", destination: `${backendOrigin}/feed.xml` }];
  },
  images: {
    // next/image 经优化代理加载这些远端图片；后端 /uploads 与 /files 都由 gin 静态提供。
    remotePatterns,
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
