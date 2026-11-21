import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'

import { router } from '@/router'
import { createAppQueryClient } from '@/api/query-client'
import { TooltipProvider } from '@/components/ui/tooltip'
import { useThemeStore } from '@/stores/theme'
import { getUiState, loadUiState, updateUiState } from '@/stores/ui-state'
import '@/styles/globals.css'

const queryClient = createAppQueryClient()

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

function render() {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </QueryClientProvider>
    </StrictMode>
  )
}

async function start() {
  // The device UI state (NSFW, player volume, immersive size) is loaded before
  // the first render: no NSFW flash and no default volume applied after the
  // player has already been created.
  await loadUiState()
  // The desktop shell owns the durable theme in `ui-state.json`; localStorage is
  // only its mirror. The shell picks a fresh port on every start, so the WebView
  // origin (and with it localStorage) changes — seeding from the durable copy
  // keeps the chosen theme across restarts, and the subscription writes it back.
  useThemeStore.setState({ theme: getUiState().theme })
  useThemeStore.subscribe(state => updateUiState({ theme: state.theme }))
  render()
}

void start()
