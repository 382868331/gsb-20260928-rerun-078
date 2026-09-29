// Stable identity generators. IDs are never regenerated on save,
// undo or import: they are the join key for links and rules.
let counter = 0;

export function uid(prefix: string): string {
  counter += 1;
  const rand = Math.random().toString(36).slice(2, 8);
  return `${prefix}_${Date.now().toString(36)}_${counter}_${rand}`;
}

export function clone<T>(v: T): T {
  return structuredClone(v);
}
