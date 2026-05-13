# New Workspace

This directory is now the workspace root for the extracted system.

## Layout

```text
new/
  backend/
    data-model-service/   current Go backend service
  frontend/               frontend app placeholder
```

## Current backend

All backend work completed so far lives in:

- `backend/data-model-service`

That service contains:

- the Go module
- the HTTP API
- metadata migrations
- tenant schema management
- Docker and local run files
- service docs and handoff notes

## Frontend

The frontend directory has been created as the next workspace area:

- `frontend/`

It is currently a placeholder so frontend work can be added without mixing it into the backend service directory.

## Next step

If you want to work on the current service, use:

```powershell
Set-Location "C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service"
```
