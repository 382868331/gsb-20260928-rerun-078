import { useState } from "react";
import { api, normalizeTopology } from "../lib/api";
import { loadCheckForVersion, type CachedCheck } from "../lib/checkCache";
import { useVersions } from "../lib/versions";
import { useTopoStore } from "../state/topoStore";

interface Props {
  onScenarioLoaded: (check: CachedCheck | null) => void;
}

// ScenarioBar switches between saved policy scenarios (immutable
// versions). Switching with unsaved edits never silently drops or
// overwrites anything: the user explicitly chooses to save the draft
// as a new version, discard it, or stay.
export function ScenarioBar({ onScenarioLoaded }: Props) {
  const topo = useTopoStore((s) => s.topo);
  const dirty = useTopoStore((s) => s.editSeq !== s.cleanEditSeq);
  const loadSnapshot = useTopoStore((s) => s.loadSnapshot);
  const { versions } = useVersions();
  const [pending, setPending] = useState<number | null>(null);
  const [msg, setMsg] = useState("");

  async function applySwitch(v: number) {
    setMsg("");
    try {
      const snap = await api.loadVersion(v);
      const t = normalizeTopology(snap.topology);
      loadSnapshot(t);
      onScenarioLoaded(loadCheckForVersion(t.version));
      setMsg(`已切换到方案 v${t.version}`);
    } catch (e) {
      setMsg((e as Error).message);
    }
  }

  function requestSwitch(v: number) {
    if (v === topo.version && !dirty) return;
    if (dirty) {
      setPending(v); // unsaved draft: require an explicit decision
    } else {
      void applySwitch(v);
    }
  }

  async function saveThenSwitch() {
    if (pending === null) return;
    const label = prompt("将当前修改保存为新版本（不会覆盖历史方案）", `切换前保存 ${new Date().toLocaleString()}`);
    if (label === null) return;
    try {
      const snap = await api.save(topo, label);
      loadSnapshot(normalizeTopology(snap.topology));
      const target = pending;
      setPending(null);
      await applySwitch(target);
    } catch (e) {
      setMsg((e as Error).message);
    }
  }

  function discardThenSwitch() {
    if (pending === null) return;
    const target = pending;
    setPending(null);
    void applySwitch(target);
  }

  return (
    <div className="scenario-bar">
      <label>
        策略方案：
        <select
          value={dirty ? "draft" : String(topo.version)}
          onChange={(e) => {
            const v = Number(e.target.value);
            if (Number.isFinite(v)) requestSwitch(v);
          }}
        >
          {dirty && <option value="draft">未保存修改（基于 v{topo.version || "空"}）</option>}
          {versions.length === 0 && !dirty && <option value="0">尚无已保存方案</option>}
          {versions.map((m) => (
            <option key={m.version} value={m.version}>
              v{m.version} · {m.label || "（无备注）"}
            </option>
          ))}
        </select>
      </label>
      {dirty && <span className="dirty-tag">有未保存修改</span>}
      {msg && <span className="msg">{msg}</span>}
      {pending !== null && (
        <div className="draft-dialog">
          <div>
            当前草稿有未保存修改。切换到方案 v{pending} 前请选择处理方式（历史方案不会被覆盖）：
          </div>
          <div className="draft-dialog-actions">
            <button className="primary" onClick={saveThenSwitch}>保存为新版本并切换</button>
            <button onClick={discardThenSwitch}>丢弃修改并切换</button>
            <button onClick={() => setPending(null)}>取消</button>
          </div>
        </div>
      )}
    </div>
  );
}
