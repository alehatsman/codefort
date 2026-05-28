import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import { keys } from "./queries"
import type {
  ClaimIssueInput,
  CreateCommentInput,
  CreateIssueInput,
  UpdateIssueInput,
} from "./types"

// Helper: invalidate everything that's affected by a write to one issue.
// A new/updated issue can change repo counts and issue lists; comment
// writes only touch the comments query.
function invalidateIssueWrites(
  qc: ReturnType<typeof useQueryClient>,
  owner: string,
  repo: string,
  n?: number
) {
  qc.invalidateQueries({ queryKey: keys.issues(owner, repo) })
  qc.invalidateQueries({ queryKey: keys.repos() })
  qc.invalidateQueries({ queryKey: keys.repo(owner, repo) })
  if (n !== undefined) {
    qc.invalidateQueries({ queryKey: keys.issue(owner, repo, n) })
  }
}

export function useCreateIssue(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateIssueInput) => api.createIssue(owner, repo, input),
    onSuccess: () => invalidateIssueWrites(qc, owner, repo),
  })
}

export function useSetIssueState(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: UpdateIssueInput) => api.updateIssue(owner, repo, n, input),
    onSuccess: () => invalidateIssueWrites(qc, owner, repo, n),
  })
}

export function useClaimIssue(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: ClaimIssueInput) => api.claimIssue(owner, repo, n, input),
    onSuccess: () => invalidateIssueWrites(qc, owner, repo, n),
  })
}

export function useUnclaimIssue(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.unclaimIssue(owner, repo, n),
    onSuccess: () => invalidateIssueWrites(qc, owner, repo, n),
  })
}

export function useCreateComment(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateCommentInput) => api.createComment(owner, repo, n, input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.comments(owner, repo, n) })
    },
  })
}

export function useDeleteComment(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (commentID: number) => api.deleteComment(owner, repo, n, commentID),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.comments(owner, repo, n) })
    },
  })
}
