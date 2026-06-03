import clsx from "clsx"
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react"

type ToastVariant = "info" | "success" | "error"

interface ToastOptions {
  variant?: ToastVariant
  /** Auto-dismiss after this many ms; `0` keeps it until dismissed. Default 4000. */
  duration?: number
}

interface ToastRecord {
  id: number
  message: ReactNode
  variant: ToastVariant
}

/** Dispatch a toast; returns its id (pass to nothing — dismissal is automatic or via the ×). */
type ToastFn = (message: ReactNode, options?: ToastOptions) => number

const ToastContext = createContext<ToastFn | null>(null)

/**
 * Provides {@link useToast} and renders the toast stack. Wrap the app once,
 * above the routes. Toasts stack bottom-right, auto-dismiss, and live in an
 * aria-live region (errors announce assertively) so they're accessible.
 * Dependency-free: React context + a fixed-position stack, no portal lib.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastRecord[]>([])
  const idRef = useRef(0)
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>())

  const dismiss = useCallback((id: number) => {
    setToasts((list) => list.filter((t) => t.id !== id))
    const timer = timers.current.get(id)
    if (timer) {
      clearTimeout(timer)
      timers.current.delete(id)
    }
  }, [])

  const toast = useCallback<ToastFn>(
    (message, options) => {
      idRef.current += 1
      const id = idRef.current
      setToasts((list) => [...list, { id, message, variant: options?.variant ?? "info" }])
      const duration = options?.duration ?? 4000
      if (duration > 0)
        timers.current.set(
          id,
          setTimeout(() => dismiss(id), duration)
        )
      return id
    },
    [dismiss]
  )

  // Clear pending timers on unmount.
  useEffect(() => {
    const map = timers.current
    return () => {
      for (const t of map.values()) clearTimeout(t)
    }
  }, [])

  return (
    <ToastContext.Provider value={toast}>
      {children}
      <section className="toast-stack" aria-label="Notifications">
        {toasts.map((t) => (
          <div
            key={t.id}
            role={t.variant === "error" ? "alert" : "status"}
            className={clsx("toast", `toast--${t.variant}`)}
          >
            <span className="toast__msg">{t.message}</span>
            <button
              type="button"
              className="toast__close"
              onClick={() => dismiss(t.id)}
              aria-label="Dismiss notification"
            >
              ×
            </button>
          </div>
        ))}
      </section>
    </ToastContext.Provider>
  )
}

/** Returns the toast dispatcher. Must be called under a {@link ToastProvider}. */
export function useToast(): ToastFn {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error("useToast must be used within a ToastProvider")
  return ctx
}
