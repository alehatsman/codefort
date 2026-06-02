// timeAgo renders an ISO timestamp as a short relative string ("3 days ago",
// "just now") via Intl.RelativeTimeFormat. Pair it with absoluteTime() for a
// `title` tooltip so the exact time is one hover away.

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })

const DIVISIONS: { amount: number; unit: Intl.RelativeTimeFormatUnit }[] = [
  { amount: 60, unit: "second" },
  { amount: 60, unit: "minute" },
  { amount: 24, unit: "hour" },
  { amount: 7, unit: "day" },
  { amount: 4.34524, unit: "week" },
  { amount: 12, unit: "month" },
  { amount: Number.POSITIVE_INFINITY, unit: "year" },
]

export function timeAgo(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ""
  let duration = (date.getTime() - Date.now()) / 1000 // seconds, negative for past
  for (const division of DIVISIONS) {
    if (Math.abs(duration) < division.amount) {
      return rtf.format(Math.round(duration), division.unit)
    }
    duration /= division.amount
  }
  return ""
}

export function absoluteTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ""
  return date.toLocaleString()
}
