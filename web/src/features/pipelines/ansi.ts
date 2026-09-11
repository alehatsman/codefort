// Minimal ANSI SGR parser for CI log lines.
//
// Build tools (go, biome, vitest, …) emit SGR color escapes; rendered raw they
// leak `\x1b[…m` noise into the log. This turns one line into styled segments
// the log viewer maps to spans. It interprets the subset that actually shows up
// in build output — the 8 standard + 8 bright foreground colors, bold, dim,
// underline, and resets — and silently drops every other CSI sequence (cursor
// moves, the `\x1b[K` erase-line that go's download progress emits, bg colors)
// so they don't render as garbage.
//
// Foreground colors map to the theme's semantic tokens rather than fixed RGB,
// so log color tracks the active theme and stays legible on every background.

export type AnsiColor = "black" | "red" | "green" | "yellow" | "blue" | "magenta" | "cyan" | "white"

export interface AnsiSegment {
  text: string
  fg?: AnsiColor | undefined
  bold?: boolean | undefined
  dim?: boolean | undefined
  underline?: boolean | undefined
}

interface SgrState {
  fg?: AnsiColor | undefined
  bold?: boolean | undefined
  dim?: boolean | undefined
  underline?: boolean | undefined
}

const FG: Record<number, AnsiColor> = {
  30: "black",
  31: "red",
  32: "green",
  33: "yellow",
  34: "blue",
  35: "magenta",
  36: "cyan",
  37: "white",
}

// biome-ignore lint/suspicious/noControlCharactersInRegex: matching the literal ESC that opens a CSI sequence is the point
const CSI = /\x1b\[([0-9;]*)([A-Za-z])/g

// parseAnsi splits a line into styled segments. A line with no escapes returns
// a single plain segment (the common case — fast path).
export function parseAnsi(line: string): AnsiSegment[] {
  if (!line.includes("\x1b[")) return [{ text: line }]

  const segments: AnsiSegment[] = []
  const state: SgrState = {}
  let last = 0

  CSI.lastIndex = 0
  for (let m = CSI.exec(line); m !== null; m = CSI.exec(line)) {
    const text = line.slice(last, m.index)
    if (text) segments.push({ text, ...state })
    last = CSI.lastIndex
    // Only SGR ("m") changes style; every other final byte is a control
    // sequence we strip without rendering.
    if (m[2] === "m") applySgr(state, m[1] ?? "")
  }

  const tail = line.slice(last)
  if (tail) segments.push({ text: tail, ...state })
  return segments
}

// applySgr folds one SGR parameter list into the running style. An empty
// parameter list (`\x1b[m`) is a reset, matching the terminal convention.
function applySgr(state: SgrState, params: string): void {
  const codes = params === "" ? [0] : params.split(";").map((p) => Number(p) || 0)
  for (const code of codes) applySgrCode(state, code)
}

// applySgrCode folds one SGR code into the running style. A switch, not an
// if/else-if chain, so the code-by-code dispatch doesn't stack cognitive
// complexity on top of the caller's loop.
function applySgrCode(state: SgrState, code: number): void {
  switch (code) {
    case 0:
      state.fg = undefined
      state.bold = undefined
      state.dim = undefined
      state.underline = undefined
      return
    case 1:
      state.bold = true
      return
    case 2:
      state.dim = true
      return
    case 4:
      state.underline = true
      return
    case 22:
      state.bold = undefined
      state.dim = undefined
      return
    case 24:
      state.underline = undefined
      return
    case 39:
      state.fg = undefined
      return
  }
  if (code in FG) {
    state.fg = FG[code]
    return
  }
  // Bright foregrounds (90–97) share the base color; brightness reads as bold.
  if (code >= 90 && code <= 97) {
    state.fg = FG[code - 60]
    state.bold = true
  }
  // Everything else (backgrounds, 256/truecolor, blink, …) is ignored.
}
