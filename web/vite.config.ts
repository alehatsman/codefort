import { fileURLToPath, URL } from "node:url"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// Dev posture: Vite serves the UI on :5173 and proxies /api/* to the Go
// daemon (default :8080). Same-origin in the browser — no CORS to wire up.
// CODEFORT_API_TARGET repoints the proxy at another backend — e.g. a throwaway
// codefortd instance on another port when dogfooding a server change.
const apiTarget = process.env.CODEFORT_API_TARGET || "http://localhost:8080"

export default defineConfig({
  plugins: [react()],
  resolve: {
    // "@/..." resolves to src/... — see tsconfig paths. Keeps imports flat and
    // move-proof across the feature folders.
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: apiTarget,
        changeOrigin: false,
      },
    },
  },
})
