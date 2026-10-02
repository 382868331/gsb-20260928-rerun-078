import { useCallback, useEffect, useState } from "react";
import { api } from "./api";
import { useTopoStore } from "../state/topoStore";

export interface VersionMeta {
  version: number;
  label: string;
  created: string;
}

// useVersions lists saved immutable versions (the "scenarios") and
// refreshes whenever the current draft's version changes (i.e. after
// a save or a scenario switch).
export function useVersions() {
  const currentVersion = useTopoStore((s) => s.topo.version);
  const [versions, setVersions] = useState<VersionMeta[]>([]);
  const refresh = useCallback(() => {
    // The backend guarantees [], but coerce defensively: a null here
    // used to crash every .map over the list and blank the page.
    api
      .versions()
      .then((v) => setVersions(Array.isArray(v) ? v : []))
      .catch(() => setVersions([]));
  }, []);
  useEffect(refresh, [refresh, currentVersion]);
  return { versions, refresh };
}
