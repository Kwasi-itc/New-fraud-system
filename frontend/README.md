# Frontend

This directory now contains a minimal Next.js App Router scaffold with:

- Zustand for local client state
- TanStack Query for server-state fetching and caching
- a sample API route at `app/api/health/route.ts`
- a shared provider at `components/providers.tsx`

## Run

1. `npm install`
2. `npm run dev`

## Structure

- `app/`
  App Router entrypoints and API routes
- `components/providers.tsx`
  Shared client-side providers
- `lib/query-client.ts`
  TanStack Query client factory
- `stores/ui-store.ts`
  Sample Zustand store
