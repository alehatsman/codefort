import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { BrowserRouter } from "react-router-dom"
import App from "./App"
import "./styles.css"

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (failureCount, err: unknown) => {
        // Don't retry 4xx client errors (auth, not-found, bad request, …) —
        // they won't fix themselves, and retrying just multiplies the request.
        if (err && typeof err === "object" && "status" in err) {
          const status = (err as { status: number }).status
          if (status >= 400 && status < 500) return false
        }
        return failureCount < 2
      },
    },
  },
})

const rootEl = document.getElementById("root")
if (!rootEl) throw new Error("root element #root not found")

createRoot(rootEl).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>
)
