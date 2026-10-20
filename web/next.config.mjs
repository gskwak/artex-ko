import createNextIntlPlugin from "next-intl/plugin";

import { fileURLToPath } from "node:url";

// Static export: `NEXT_EXPORT=1 next build` writes a static site to web/out,
// ready for the nginx web root. Development keeps the /api proxy and hot reload.
const isExport = process.env.NEXT_EXPORT === "1";
// The Vercel demo uses mock data throughout and needs no backend or /api proxy.
const isMock = process.env.NEXT_PUBLIC_MOCK === "1";

/** @type {import('next').NextConfig} */
const nextConfig = {
  // Keep a parent lockfile from affecting root detection and asset paths.
  turbopack: { root: fileURLToPath(new URL(".", import.meta.url)) },
  reactCompiler: true,
  // Allow LAN IPv4 clients to access dev assets and HMR as their addresses change.
  // Next rejects a bare "*"; "*.*.*.*" matches any IPv4 address.
  allowedDevOrigins: ["*.*.*.*"],
  compiler: {
    removeConsole: process.env.NODE_ENV === "production",
  },
  ...(isExport
    ? {
        // Static export needs no Node runtime or image optimizer; each route writes <route>/index.html.
        output: "export",
        images: { unoptimized: true },
        trailingSlash: true,
      }
    : isMock
      ? {
          // The Vercel mock demo needs no backend or /api proxy.
          images: { unoptimized: true },
        }
      : {
          // In development, proxy /api/* to the Go backend (default :8787; override with AUTOPENTEST_API).
          async rewrites() {
            const backend = process.env.AUTOPENTEST_API ?? "http://localhost:8787";
            return [{ source: "/api/:path*", destination: `${backend}/api/:path*` }];
          },
        }),
};

// next-intl 플러그인. 요청 설정은 src/i18n/request.ts 에 둔다. i18n 경로 라우팅을
// 쓰지 않으므로 미들웨어는 추가하지 않는다(정적 내보내기 output: "export" 와 호환).
const withNextIntl = createNextIntlPlugin("./src/i18n/request.ts");

export default withNextIntl(nextConfig);
