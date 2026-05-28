// Hand-mirrored from internal/api/types.go. Keep in sync.
// (Future: generate from `mooncake schema generate` once we expose
// moongit's API schema there.)

export type IssueState = "todo" | "in_progress" | "done" | "closed";

export interface Repo {
  id: number;
  owner: string;
  name: string;
  created_at: string;
  open_issues: number;
  total_issues: number;
}

export interface Issue {
  id: number;
  number: number;
  title: string;
  body?: string;
  author: string;
  state: IssueState;
  assignee: string | null;
  created_at: string;
  updated_at: string;
}

export interface Comment {
  id: number;
  issue_id: number;
  author: string;
  body: string;
  created_at: string;
}

export interface ErrorResponse {
  error: string;
}
