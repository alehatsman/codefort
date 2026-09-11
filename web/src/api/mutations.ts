import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/api/client"
import { keys } from "@/api/queries"
import type {
  AddMemberInput,
  CIRunExecutionModel,
  CIRunToolProfile,
  ClaimIssueInput,
  CreateCodeCommentInput,
  CreateCommentInput,
  CreateIssueInput,
  CreatePullRequestInput,
  CreateRepoInput,
  CreateSSHKeyInput,
  CreateTokenInput,
  LoginInput,
  MergeRequestInput,
  RegisterInput,
  UpdateAgentSettingsInput,
  UpdateIssueInput,
  UpdatePullRequestInput,
  UpdateRepoInput,
} from "@/api/types"

export function useCreateRepo() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateRepoInput) => api.createRepo(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.repos() }),
  })
}

// useDeleteRepo hard-deletes a repo and everything under it. On success the
// repo is gone, so there's no per-repo cache worth keeping — invalidate the
// repos list (the only place the now-deleted repo could still show up).
export function useDeleteRepo() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ owner, repo }: { owner: string; repo: string }) => api.deleteRepo(owner, repo),
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

export function useAddSSHKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateSSHKeyInput) => api.createSSHKey(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.sshKeys() }),
  })
}

export function useDeleteSSHKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.deleteSSHKey(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.sshKeys() }),
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

// useCreateIssue creates an issue in a repo chosen at submit time — owner/repo
// travel in the mutate variables rather than being bound at hook init, so the
// same hook serves both the repo-scoped form (target fixed by the route) and
// the global Issues view (target picked from a dropdown).
export function useCreateIssue() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ owner, repo, ...input }: CreateIssueInput & { owner: string; repo: string }) =>
      api.createIssue(owner, repo, input),
    onSuccess: (_created, { owner, repo }) => invalidateIssueWrites(qc, owner, repo),
  })
}

// Partial issue update — state, title, and/or body. Used both by the sidebar
// state picker and the inline title/body editor.
export function useUpdateIssue(owner: string, repo: string, n: number) {
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
      void qc.invalidateQueries({ queryKey: keys.comments(owner, repo, n) })
    },
  })
}

export function useDeleteComment(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (commentID: number) => api.deleteComment(owner, repo, n, commentID),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.comments(owner, repo, n) })
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

// Pull requests. The pulls key is hierarchical (["pulls", owner, repo, state]),
// so invalidating the owner/repo prefix refreshes every state-filtered list at
// once; a write touching one PR also refreshes its detail query.
function invalidatePullWrites(
  qc: ReturnType<typeof useQueryClient>,
  owner: string,
  repo: string,
  n?: number
) {
  qc.invalidateQueries({ queryKey: ["pulls", owner, repo] })
  if (n !== undefined) {
    qc.invalidateQueries({ queryKey: keys.pull(owner, repo, n) })
  }
}

export function useCreatePull(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreatePullRequestInput) => api.createPull(owner, repo, input),
    onSuccess: () => invalidatePullWrites(qc, owner, repo),
  })
}

export function useUpdatePull(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: UpdatePullRequestInput) => api.updatePull(owner, repo, n, input),
    onSuccess: () => invalidatePullWrites(qc, owner, repo, n),
  })
}

// useMergePull merges the PR; on success the base branch advanced, so refresh
// the PR (now merged) and any branch-derived views (commits, compare).
export function useMergePull(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: MergeRequestInput) => api.mergePull(owner, repo, n, input),
    onSuccess: () => {
      invalidatePullWrites(qc, owner, repo, n)
      void qc.invalidateQueries({ queryKey: ["commits", owner, repo] })
      void qc.invalidateQueries({ queryKey: ["compare", owner, repo] })
    },
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

// useSpawnAgent starts an agent run for an issue. The agent run shares the
// ci_runs surface, so refresh the runs list once it's accepted (the run view
// lives under Pipelines). The optional vars pick the base ref and execution
// model; the server fills defaults for anything omitted.
export function useSpawnAgent(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (vars?: {
      ref?: string
      model?: CIRunExecutionModel
      toolProfile?: CIRunToolProfile
    }) => api.spawnAgent(owner, repo, n, vars),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.ciRuns(owner, repo) }),
  })
}

// useDraftReviewAgent creates a review issue from a draft and immediately spawns
// a read-only review agent against it (#159): one click on the Review tab turns
// "review this target" into an issue + a running agent scoped to the "review"
// tool profile (read + review_* only). Returns the spawned run so the caller can
// navigate to its live transcript. claude-edit is pinned because it reliably
// unlocks read tools + MCP under headless bypassPermissions (#110).
export function useDraftReviewAgent(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: CreateIssueInput) => {
      const issue = await api.createIssue(owner, repo, input)
      const run = await api.spawnAgent(owner, repo, issue.number, {
        model: "claude-edit",
        toolProfile: "review",
      })
      return { issue, run }
    },
    onSuccess: () => {
      invalidateIssueWrites(qc, owner, repo)
      void qc.invalidateQueries({ queryKey: keys.ciRuns(owner, repo) })
    },
  })
}

// useCreateAgentTurn queues a follow-up message on an agent run; once accepted,
// refresh the run so the queued turn shows and the transcript starts tailing.
export function useCreateAgentTurn(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (text: string) => api.createAgentTurn(owner, repo, n, text),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.ciRun(owner, repo, n) }),
  })
}

// useUpdateAgentSettings sets/clears the global agent Claude token.
export function useUpdateAgentSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: UpdateAgentSettingsInput) => api.updateAgentSettings(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.agentSettings() }),
  })
}

// useFinishAgentRun accepts a parked agent run; once accepted, refresh the run
// so it shows finishing/finished.
export function useFinishAgentRun(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.finishAgentRun(owner, repo, n),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.ciRun(owner, repo, n) }),
  })
}

// useCancelAgentRun force-stops a running agent run; once accepted, refresh the
// run so it shows canceled.
export function useCancelAgentRun(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.cancelRun(owner, repo, n),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.ciRun(owner, repo, n) }),
  })
}

// useCancelCIRun force-stops a running or queued CI run (#296); once accepted,
// refresh the run so it shows canceled. Same endpoint as the agent cancel — the
// server picks the path by the run's kind.
export function useCancelCIRun(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.cancelRun(owner, repo, n),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.ciRun(owner, repo, n) }),
  })
}

// useTriggerCIRun starts a run for an arbitrary ref without a push; like a
// rerun, the fresh run tops the list, so refresh it once accepted.
export function useTriggerCIRun(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ref: string) => api.triggerCIRun(owner, repo, ref),
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
      void qc.invalidateQueries({ queryKey: keys.repo(owner, repo) })
      void qc.invalidateQueries({ queryKey: keys.repos() })
    },
  })
}

// useSetRepoVisibility toggles a repo between public and private.
export function useSetRepoVisibility(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (visibility: "public" | "private") =>
      api.updateRepo(owner, repo, { visibility } as UpdateRepoInput),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.repo(owner, repo) })
      void qc.invalidateQueries({ queryKey: keys.repos() })
    },
  })
}

// useRegister creates a new user account and returns an AuthResponse.
export function useRegister() {
  return useMutation({
    mutationFn: (input: RegisterInput) => api.register(input),
  })
}

// useLogin authenticates a user and returns an AuthResponse.
export function useLogin() {
  return useMutation({
    mutationFn: (input: LoginInput) => api.login(input),
  })
}

// useAddRepoMember grants a collaborator access to a repo.
export function useAddRepoMember(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: AddMemberInput) => api.addRepoMember(owner, repo, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.repoMembers(owner, repo) }),
  })
}

// useRemoveRepoMember revokes a collaborator's access.
export function useRemoveRepoMember(owner: string, repo: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (username: string) => api.removeRepoMember(owner, repo, username),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.repoMembers(owner, repo) }),
  })
}

// useSubmitReview posts approve or request-changes for the current user on a PR.
export function useSubmitReview(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (state: import("./types").PRReviewState) => api.submitReview(owner, repo, n, state),
    onSuccess: () => invalidatePullWrites(qc, owner, repo, n),
  })
}

export function useCreateBranch(owner: string, repo: string) {
  return useMutation({
    mutationFn: (body: { name: string; base?: string | undefined }) =>
      api.createBranch(owner, repo, body),
  })
}

export function useAddDependency(owner: string, repo: string, number: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (dependsOn: number) => api.addDependency(owner, repo, number, dependsOn),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.issue(owner, repo, number) }),
  })
}

export function useRemoveDependency(owner: string, repo: string, number: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (target: number) => api.removeDependency(owner, repo, number, target),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.issue(owner, repo, number) }),
  })
}
