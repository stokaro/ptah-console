/** Shortens a digest or fingerprint to its algorithm, head and tail. */
export function shortDigest(value: string, keep = 4) {
  const match = /^([a-z0-9]+:)?([0-9a-f]+)$/i.exec(value)
  if (!match || match[2].length <= keep * 2 + 1) {
    return value
  }
  const [, algorithm = "", hex] = match
  return `${algorithm}${hex.slice(0, keep)}…${hex.slice(-keep)}`
}
