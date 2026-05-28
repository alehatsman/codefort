import type { Comment, Issue, Repo } from "./types";

const TOKEN_KEY = "moongit_token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

/** ApiError carries the HTTP status so callers can branch on 401 etc. */
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = getToken();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const resp = await fetch(path, { ...init, headers });
  const raw = await resp.text();
  if (!resp.ok) {
    let msg = raw;
    try {
      msg = JSON.parse(raw).error ?? raw;
    } catch {
      // raw stays as-is
    }
    throw new ApiError(resp.status, msg || resp.statusText);
  }
  if (!raw) return undefined as T;
  return JSON.parse(raw) as T;
}

export const api = {
  listRepos: () => request<Repo[]>("/api/repos"),
  getRepo: (owner: string, repo: string) =>
    request<Repo>(`/api/repos/${owner}/${repo}`),
  listIssues: (owner: string, repo: string, query: string = "") =>
    request<Issue[]>(`/api/repos/${owner}/${repo}/issues${query ? `?${query}` : ""}`),
  getIssue: (owner: string, repo: string, n: number) =>
    request<Issue>(`/api/repos/${owner}/${repo}/issues/${n}`),
  listComments: (owner: string, repo: string, n: number) =>
    request<Comment[]>(`/api/repos/${owner}/${repo}/issues/${n}/comments`),
};
