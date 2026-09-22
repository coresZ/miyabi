export function clamp(value: number, min: number, max: number): number {
  const clamped = Math.min(Math.max(value, min), max)
  return clamped === 0 ? 0 : clamped
}
