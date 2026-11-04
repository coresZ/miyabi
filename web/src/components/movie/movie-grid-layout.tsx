import type { ReactNode } from 'react'

export function MovieGridLayout({ children }: { children: ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 md:grid-cols-3 lg:grid-cols-4 lg:gap-5 xl:grid-cols-5">
      {children}
    </div>
  )
}
