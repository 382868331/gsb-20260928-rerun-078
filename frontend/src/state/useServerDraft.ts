import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { useTopoStore } from "./topoStore";

// On first open, adopt the server-side draft (latest saved version).
// Reopening the same version therefore shows the exact same topology;
// the deterministic backend then returns the identical explanation.
export function useServerDraft() {
  const replaceAll = useTopoStore((s) => s.replaceAll);
  const [status, setStatus] = useState<"loading" | "ready" | "offline">("loading");

  useEffect(() => {
    let cancelled = false;
    api
      .getDraft()
      .then((t) => {
        if (!cancelled && t && Array.isArray(t.nodes)) {
          replaceAll(t, { keepHistory: false });
          setStatus("ready");
        }
      })
      .catch(() => {
        if (!cancelled) setStatus("offline");
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return status;
}
