import { useState } from "react"
import "./agents.css"
import { useNavigate } from "react-router-dom"
import { useSpawnAgent } from "@/api/mutations"
import type { CIRunToolProfile } from "@/api/types"
import { Button, ErrorMessage, Select } from "@/ui"

interface Props {
  owner: string
  repo: string
  number: number
}

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
 * Spawns a containerized agent to work this issue via claude-edit (#110) — the
 * sole execution model, so there's nothing to pick here beyond the tool
 * profile. The agent run shares the CI run surface, so on success we navigate
 * to its run view under Pipelines, where the transcript streams live. The
 * base defaults to the repo's HEAD (the server resolves it).
 */
export default function SpawnAgentButton({ owner, repo, number }: Props) {
  const navigate = useNavigate()
  const spawn = useSpawnAgent(owner, repo, number)
  const [profile, setProfile] = useState<CIRunToolProfile>("full")

  function onSpawn() {
    spawn.mutate(
      { toolProfile: profile },
      { onSuccess: (run) => navigate(`/${owner}/${repo}/agents/${run.number}`) }
    )
  }

  const profileHint = PROFILES.find((p) => p.value === profile)?.hint

  return (
    <>
      <div className="spawn-agent">
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
      <p className="muted small">
        {profileHint} Runs in an isolated container; progress streams under Pipelines.
      </p>
      <ErrorMessage error={spawn.error} inline />
    </>
  )
}
