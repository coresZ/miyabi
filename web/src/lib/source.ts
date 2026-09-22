export type SourceScope =
  | { account_id: string; directory_id: string }
  | { account_id: string; directory: { id: string } }

export function sameSource(a?: SourceScope | null, b?: SourceScope | null): boolean {
  if (!a || !b) return false
  const dirA = 'directory_id' in a ? a.directory_id : a.directory.id
  const dirB = 'directory_id' in b ? b.directory_id : b.directory.id
  return a.account_id === b.account_id && dirA === dirB
}

export function sourceKey(source?: SourceScope | null): string {
  if (!source) return ''
  const dir = 'directory_id' in source ? source.directory_id : source.directory.id
  return `${source.account_id}:${dir}`
}
