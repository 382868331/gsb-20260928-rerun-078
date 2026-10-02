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
    // Guard against a null/non-array payload (older backends serialize
    // an empty list as null): the page must never blank over it.
    api
      .versions()
      .then((v) => setVersions(Array.isArray(v) ? v : []))
      .catch(() => setVersions([]));
  }, []);
  useEffect(refresh, [refresh, currentVersion]);
  return { versions, refresh };
}
