import { defineConfig } from "vite";
import preact from "@preact/preset-vite";
import { VitePWA } from "vite-plugin-pwa";
import { resolve } from "path";

export default defineConfig({
  plugins: [
    preact(),
    VitePWA({
      // autoUpdate + skipWaiting + clientsClaim makes a freshly-installed
      // SW take over existing tabs immediately and triggers a page reload,
      // so users always run the deployed code without having to click a
      // snackbar. Tradeoff: an active terminal page reloads when we ship —
      // tmux state survives on the host, the SPA reconnects automatically
      // via the reconnect supervisor.
      registerType: "autoUpdate",
      includeAssets: ["icons/*.png", "icons/*.svg", "apple-touch-icon.png"],
      manifest: {
        name: "Termix",
        short_name: "Termix",
        description: "Remote terminal control for tmux sessions",
        theme_color: "#1A1A1A",
        background_color: "#F5F2EA",
        display: "standalone",
        orientation: "any",
        start_url: "/",
        scope: "/",
        icons: [
          { src: "/icons/termix.svg", sizes: "any", type: "image/svg+xml" },
          { src: "/icons/icon-192.png", sizes: "192x192", type: "image/png" },
          { src: "/icons/icon-512.png", sizes: "512x512", type: "image/png" },
          { src: "/icons/icon-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
        ],
      },
      workbox: {
        skipWaiting: true,
        clientsClaim: true,
        navigateFallback: "/index.html",
        navigateFallbackDenylist: [/^\/api\//, /^\/ws\//],
        // No runtimeCaching for /assets/ — the vite-plugin-pwa precache
        // manifest already ships every hashed bundle on SW install, and a
        // StaleWhileRevalidate runtime cache only kept the *previous* deploy's
        // assets alive for one extra page-load, defeating the immutable-hash
        // contract and forcing two refreshes to pick up a new build.
      },
      devOptions: { enabled: false },
    }),
  ],
  base: "/",
  resolve: {
    alias: { "@": resolve(__dirname, "src") },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: { input: resolve(__dirname, "index.html") },
    sourcemap: false,
  },
  server: {
    proxy: {
      "/api/v1": "http://localhost:8080",
      "/ws":     { target: "ws://localhost:8090", ws: true },
    },
  },
  test: {
    environment: "happy-dom",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
  },
});
