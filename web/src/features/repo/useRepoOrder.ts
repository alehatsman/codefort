import { useState } from "react"
import type { Repo } from "@/api/types"

const STORAGE_KEY = "codefort:repo-order"

function loadOrder(): string[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function saveOrder(order: string[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(order))
  } catch {
    // localStorage unavailable — silently ignore
  }
}

// Applies the saved order to a list of repos. Known repos are sorted by the
// saved key ("owner/name") order; new repos (not in the saved list) are
// appended at the end in their original API order.
export function applyOrder(repos: Repo[], order: string[]): Repo[] {
  if (order.length === 0) return repos
  const indexed = new Map(order.map((key, i) => [key, i]))
  return [...repos].sort((a, b) => {
    const ka = `${a.owner}/${a.name}`
    const kb = `${b.owner}/${b.name}`
    const ia = indexed.get(ka) ?? Infinity
    const ib = indexed.get(kb) ?? Infinity
    return ia - ib
  })
}

export function useRepoOrder() {
  const [order, setOrder] = useState<string[]>(loadOrder)

  function reorder(newOrder: string[]) {
    setOrder(newOrder)
    saveOrder(newOrder)
  }

  function resetOrder() {
    setOrder([])
    try {
      localStorage.removeItem(STORAGE_KEY)
    } catch {
      // ignore
    }
  }

  return { order, reorder, resetOrder, isCustom: order.length > 0 }
}
