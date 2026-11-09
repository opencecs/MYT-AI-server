export function cleanRecord(result: Record<string, string | undefined | null>): Record<string, string> {
  const clean: Record<string, string> = {}
  if (result) { for (const [k, v] of Object.entries(result)) { if (v != null) clean[k] = v } }
  return clean
}
