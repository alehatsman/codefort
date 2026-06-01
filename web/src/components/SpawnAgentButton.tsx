import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { useSpawnAgent } from "../api/mutations"
import { useAgentSettings } from "../api/queries"
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
    value: "mooncake-agent",
    label: "Mooncake agent (run actions)",
    hint: "Claude plans; mooncake applies the actions, so commands run.",
  },
]

/**
 * Spawns a containerized agent to work this issue. The execution model is
 * chosen here (claude-edit vs mooncake-agent, #110) and sent with the spawn;
 * the agent run shares the CI run surface, so on success we navigate to its
 * run view under Pipelines, where the transcript streams live. The base
 * defaults to the repo's HEAD (the server resolves it).
 */
export default function SpawnAgentButton({ owner, repo, number }: Props) {
  const navigate = useNavigate()
  const spawn = useSpawnAgent(owner, repo, number)
  // The selector defaults to the operator's configured default model (Settings
  // → Agent / `agent.execution_model`); "" means the server's built-in default
  // (claude-edit). Until the user picks one explicitly, follow that default —
  // so it tracks the setting even while it loads.
  const settings = useAgentSettings()
  const serverDefault: CIRunExecutionModel = settings.data?.execution_model || "claude-edit"
  const [picked, setPicked] = useState<CIRunExecutionModel | null>(null)
  const model = picked ?? serverDefault
  const [allowShell, setAllowShell] = useState(false)
  const isMooncake = model === "mooncake-agent"

  function onSpawn() {
    spawn.mutate(
      { model, allowShell: isMooncake && allowShell },
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
            onChange={(e) => setPicked(e.target.value as CIRunExecutionModel)}
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
      {isMooncake && (
        <label className="spawn-agent__shell">
          <input
            type="checkbox"
            checked={allowShell}
            disabled={spawn.isPending}
            onChange={(e) => setAllowShell(e.target.checked)}
          />
          <span className="small">
            Allow shell commands{" "}
            <span className="muted">
              (otherwise mooncake denies <code>shell</code>/<code>cmd</code>; the agent uses typed
              actions only)
            </span>
          </span>
        </label>
      )}
      <p className="muted small">
        {hint} Runs in an isolated container; progress streams under Pipelines.
      </p>
      {spawn.error && <div className="error inline">{(spawn.error as Error).message}</div>}
    </>
  )
}
