import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

// Dev posture: Vite serves the UI on :5173 and proxies /api/* to the Go
// daemon (default :8080). Same-origin in the browser — no CORS to wire up.
// MOONGIT_API_TARGET repoints the proxy at another backend — e.g. a throwaway
// moongitd instance on another port when dogfooding a server change.
const apiTarget = process.env.MOONGIT_API_TARGET || "http://localhost:8080"

export default defineConfig({
  plugins: [react()],
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
