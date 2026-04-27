import { defineConfig } from "vite";
import { resolve } from "path";

export default defineConfig(() => ({
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: resolve(__dirname, "index.html"),
    },
    sourcemap: false,
  },
  resolve: {
    alias: { "@": resolve(__dirname, "src") },
  },
  server: {
    open: "/dev.html",
  },
  test: {
    globals: true,
    environment: "happy-dom",
    include: ["src/**/*.test.ts"],
  },
}));
