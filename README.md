# Repo Analysis Tool

A web dashboard for importing Git repositories and analyzing commit, author, file, directory, and repository-level churn metrics.

## Current status

This repository has the initial project foundation:

- Go HTTP backend skeleton using `net/http`, chi, structured logging, and SQLite migrations.
- SQLite schema for repositories, jobs, authors, aliases, commits, objects, and metric changes.
- React + TypeScript + Vite frontend skeleton using TanStack Query.
- Local runtime data is ignored through `.gitignore`.

Remote clone, ZIP upload, Git history extraction, metric aggregation, author merging, and dashboard visualizations are the next implementation milestones.

## Prerequisites

- Go 1.23 or newer.
- Node.js 18 or newer and npm.
- Git CLI available on the server host.

## Backend

```bash
go mod download
go run ./cmd/rat
```

The backend listens on `:8080` by default. Configuration can be overridden with environment variables:

```bash
cp .env.example .env
```

## Frontend

```bash
cd web
npm install
npm run dev
```

The Vite development server proxies `/api` requests to `http://localhost:8080`.

## Planned checkpoints

1. Implement managed full-history remote clone jobs.
2. Implement secure ZIP upload and repository validation.
3. Stream non-merge commits, committer timestamps, mailmap-aware authors, and Git numstat diffs.
4. Materialize file and ancestor-directory metric facts.
5. Add metric query APIs and dashboard visualizations.
6. Add author merge/undo workflows and fixture-driven correctness tests.
