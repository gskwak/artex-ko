/** Parse analyst-selected evidence IDs without accepting ambiguous or unsafe values. */
export function parseBlueEvidenceIDs(input: string, max = 12): number[] | null {
  const trimmed = input.trim();
  if (!trimmed) return [];
  if (!/^\d+(?:[\s,]+\d+)*$/.test(trimmed)) return null;

  const ids = new Set<number>();
  for (const value of trimmed.split(/[\s,]+/)) {
    const id = Number(value);
    if (!Number.isSafeInteger(id) || id <= 0) return null;
    ids.add(id);
    if (ids.size > max) return null;
  }
  return [...ids];
}
