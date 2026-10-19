import { StrictMode, useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { ThemeProvider, useTheme } from 'next-themes'

import { router } from '@/router'
import { createAppQueryClient } from '@/api/query-client'
import { TooltipProvider } from '@/components/ui/tooltip'
import { getUiState, loadUiState, updateUiState } from '@/stores/ui-state'
import '@/styles/globals.css'

const queryClient = createAppQueryClient()

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

/** Keeps the durable UI state in sync with next-themes. */
function ThemeSync() {
  const { theme } = useTheme()
  useEffect(() => {
    if (theme === 'light' || theme === 'dark' || theme === 'system') {
      updateUiState({ theme })
    }
  }, [theme])
  return null
}

function render() {
  // The desktop shell owns the real theme; localStorage is only next-themes' mirror.
  try {
    window.localStorage.setItem('miyabi-theme', getUiState().theme)
  } catch {
    // Private mode or a full quota: the durable copy still wins.
  }
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <ThemeProvider
          attribute="class"
          defaultTheme={getUiState().theme}
          storageKey="miyabi-theme"
          enableSystem
          disableTransitionOnChange
        >
          <ThemeSync />
          <TooltipProvider delayDuration={0}>
            <RouterProvider router={router} />
          </TooltipProvider>
        </ThemeProvider>
      </QueryClientProvider>
    </StrictMode>
  )
}

async function start() {
  // Load the device UI state before the first render: no NSFW flash, no
  // default volume applied after the player has already been created.
  await loadUiState()
  render()
}

void start()
