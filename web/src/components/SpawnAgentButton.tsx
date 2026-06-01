import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { useSpawnAgent } from "../api/mutations"
import type { CIRunExecutionModel } from "../api/types"

interface Props {
  owner: string
  repo: string
  number: number
}

const MODELS: { value: CIRunExecutionModel; label: string; hint: string }[] = [
  {
    value: "claude-edit",
    label: "Claude (edit files)",
    hint: "Claude edits files directly. Can't run commands.",
  },
  {
    value: "mooncake-pilot",
    label: "Mooncake pilot (run actions)",
    hint: "Claude plans; mooncake applies the actions, so commands run.",
  },
]

/**
 * Spawns a containerized agent to work this issue. The execution model is
 * chosen here (claude-edit vs mooncake-pilot, #110) and sent with the spawn;
 * the agent run shares the CI run surface, so on success we navigate to its
 * run view under Pipelines, where the transcript streams live. The base
 * defaults to the repo's HEAD (the server resolves it).
 */
export default function SpawnAgentButton({ owner, repo, number }: Props) {
  const navigate = useNavigate()
  const spawn = useSpawnAgent(owner, repo, number)
  const [model, setModel] = useState<CIRunExecutionModel>("claude-edit")

  function onSpawn() {
    spawn.mutate(
      { model },
      { onSuccess: (run) => navigate(`/${owner}/${repo}/agents/${run.number}`) }
    )
  }

  const hint = MODELS.find((m) => m.value === model)?.hint

  return (
    <>
      <div className="spawn-agent">
        <label className="spawn-agent__model">
          <span className="muted small">Model</span>
          <select
            className="select"
            value={model}
            disabled={spawn.isPending}
            onChange={(e) => setModel(e.target.value as CIRunExecutionModel)}
          >
            {MODELS.map((m) => (
              <option key={m.value} value={m.value}>
                {m.label}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          className="btn btn--primary"
          disabled={spawn.isPending}
          onClick={onSpawn}
        >
          {spawn.isPending ? "Spawning…" : "Spawn agent"}
        </button>
      </div>
      <p className="muted small">
        {hint} Runs in an isolated container; progress streams under Pipelines.
      </p>
      {spawn.error && <div className="error inline">{(spawn.error as Error).message}</div>}
    </>
  )
}
