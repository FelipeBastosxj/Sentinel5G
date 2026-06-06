# Angular Dashboard

Real-time observability frontend for the **EventStream Observability Engine**.

Stack: Angular 20+, standalone components, Angular Signals, Socket.IO,
feature-based architecture (HARDNESS §7).

## Structure

```
src/app/
  app.component.ts            ← shell (header + nav + router-outlet)
  app.config.ts               ← DI providers
  app.routes.ts               ← lazy-loaded feature routes
  core/
    components/               ← shared UI atoms (status pill, etc.)
    models/                   ← view-models
    services/                 ← realtime client (Socket.IO)
  features/
    dashboard/                ← KPIs + recent events
    events/                   ← live event table with filters
    metrics/                  ← per-channel / per-type / per-minute charts
    integrations/             ← provider health grid
```

Each feature owns its own:

* `components/` (presentational + container)
* `state/` (Signals-based stores, **never** exported)
* `*.routes.ts`

There is no global state library. Per HARDNESS §7, features must not share
mutable state. Cross-feature communication happens via the realtime stream
exposed by `core/services/realtime-client.service.ts`.

## Run locally

```powershell
cd frontend/angular-dashboard
npm install
npm start
```

The dev server starts on `http://localhost:4200` and connects to the realtime
gateway at `http://localhost:3003/ws`.
