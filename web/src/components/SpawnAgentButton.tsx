import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { useSpawnAgent } from "../api/mutations"
import { useAgentSettings } from "../api/queries"
import type { CIRunExecutionModel, CIRunToolProfile } from "../api/types"
import { Button, ErrorMessage, Select } from "./ui"

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

const PROFILES: { value: CIRunToolProfile; label: string; hint: string }[] = [
  {
    value: "full",
    label: "Full toolset",
    hint: "All mgit tools: claim issues, post comments, spawn agents, trigger pipelines.",
  },
  {
    value: "review",
    label: "Review (read-only)",
    hint: "Read + review comments only — can't claim issues, spawn agents, or trigger pipelines.",
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
  const [profile, setProfile] = useState<CIRunToolProfile>("full")

  function onSpawn() {
    spawn.mutate(
      { model, allowShell: isMooncake && allowShell, toolProfile: profile },
      { onSuccess: (run) => navigate(`/${owner}/${repo}/agents/${run.number}`) }
    )
  }

  const hint = MODELS.find((m) => m.value === model)?.hint
  const profileHint = PROFILES.find((p) => p.value === profile)?.hint

  return (
    <>
      <div className="spawn-agent">
        <label className="spawn-agent__model">
          <span className="muted small">Model</span>
          <Select
            value={model}
            disabled={spawn.isPending}
            onChange={(e) => setPicked(e.target.value as CIRunExecutionModel)}
          >
            {MODELS.map((m) => (
              <option key={m.value} value={m.value}>
                {m.label}
              </option>
            ))}
          </Select>
        </label>
        <label className="spawn-agent__model">
          <span className="muted small">Tools</span>
          <Select
            value={profile}
            disabled={spawn.isPending}
            onChange={(e) => setProfile(e.target.value as CIRunToolProfile)}
          >
            {PROFILES.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </Select>
        </label>
        <Button variant="primary" disabled={spawn.isPending} onClick={onSpawn}>
          {spawn.isPending ? "Spawning…" : "Spawn agent"}
        </Button>
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
        {hint} {profileHint} Runs in an isolated container; progress streams under Pipelines.
      </p>
      <ErrorMessage error={spawn.error} inline />
    </>
  )
}
