import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import { keys } from "./queries"
import type {
  ClaimIssueInput,
  CreateCodeCommentInput,
  CreateCommentInput,
  CreateIssueInput,
  CreateRepoInput,
  CreateTokenInput,
  UpdateIssueInput,
  UpdateRepoInput,
} from "./types"

export function useCreateRepo() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateRepoInput) => api.createRepo(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.repos() }),
  })
}

export function useCreateToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateTokenInput) => api.createToken(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.tokens() }),
  })
}

export function useRevokeToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.revokeToken(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.tokens() }),
  })
}

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

export function useDeleteIssue(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.deleteIssue(owner, repo, n),
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

// Code review comments. The codeComments key is hierarchical
// (["codeComments", owner, repo, ref, path, state]), so invalidating the
// owner/repo prefix refreshes every ref/path/state variant at once — a write
// on one branch's file view also updates the Review tab's aggregate list.
function invalidateCodeComments(
  qc: ReturnType<typeof useQueryClient>,
  owner: string,
  repo: string
) {
  qc.invalidateQueries({ queryKey: ["codeComments", owner, repo] })
}

export function useCreateCodeComment(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateCodeCommentInput) => api.createCodeComment(owner, repo, input),
    onSuccess: () => invalidateCodeComments(qc, owner, repo),
  })
}

export function useSetCodeCommentResolved(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, resolved }: { id: number; resolved: boolean }) =>
      api.updateCodeComment(owner, repo, id, { resolved }),
    onSuccess: () => invalidateCodeComments(qc, owner, repo),
  })
}

export function useDeleteCodeComment(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.deleteCodeComment(owner, repo, id),
    onSuccess: () => invalidateCodeComments(qc, owner, repo),
  })
}

// useRerunCIRun re-enqueues a run; the new run lands at the top of the list, so
// refresh the runs list once it's accepted.
export function useRerunCIRun(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (n: number) => api.rerunCIRun(owner, repo, n),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.ciRuns(owner, repo) }),
  })
}

// useSetCIEnabled flips a repo's CI opt-in and refreshes repo views (the
// Pipelines tab gates its UI on this flag).
export function useSetCIEnabled(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (enabled: boolean) =>
      api.updateRepo(owner, repo, { ci_enabled: enabled } as UpdateRepoInput),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.repo(owner, repo) })
      qc.invalidateQueries({ queryKey: keys.repos() })
    },
  })
}
