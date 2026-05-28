import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

// Dev posture: Vite serves the UI on :5173 and proxies /api/* to the Go
// daemon on :8080. Same-origin in the browser — no CORS to wire up.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
    },
  },
})
