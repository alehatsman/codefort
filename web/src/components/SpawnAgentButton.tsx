import { useNavigate } from "react-router-dom"
import { useSpawnAgent } from "../api/mutations"

interface Props {
  owner: string
  repo: string
  number: number
}

/**
 * Spawns a containerized Claude agent to work this issue. The agent run shares
 * the CI run surface, so on success we navigate to its run view under
 * Pipelines, where the transcript streams live. The base defaults to the repo's
 * HEAD (the server resolves it), so no ref input is needed here.
 */
export default function SpawnAgentButton({ owner, repo, number }: Props) {
  const navigate = useNavigate()
  const spawn = useSpawnAgent(owner, repo, number)

  function onSpawn() {
    spawn.mutate(undefined, {
      onSuccess: (run) => navigate(`/${owner}/${repo}/agents/${run.number}`),
    })
  }

  return (
    <>
      <button
        type="button"
        className="btn btn--primary"
        disabled={spawn.isPending}
        onClick={onSpawn}
      >
        {spawn.isPending ? "Spawning…" : "Spawn agent"}
      </button>
      <p className="muted small">
        Runs a Claude agent on this issue in an isolated container; progress streams under
        Pipelines.
      </p>
      {spawn.error && <div className="error inline">{(spawn.error as Error).message}</div>}
    </>
  )
}
