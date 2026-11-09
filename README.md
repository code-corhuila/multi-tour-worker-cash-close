# multi-tour-worker-cash-close

> Worker job 8: cash close (cash-reporting)

Part of the **Multi Tour** distributed system — team `multi-tour`, Grupo 2.
Governance and documentation live in [`multi-tour-docs`](https://github.com/code-corhuila/multi-tour-docs).

## What it is

Job 8 of ADR-004 D3. Every 15 minutes it lists the open cash days across tenants and, per tenant, at the end of its local calendar day, closes the
boxes that the system created by itself and warns about the boxes that were opened and not closed. It has no database and exposes only `/health`.
Spec (PROPUESTO, not signed yet): `07-api/specs/cash-reporting/multi-tour-worker-cash-close/` in `multi-tour-docs`.

## How to start it

Copy `.env.example`, fill `SERVICE_TOKEN` (never versioned) and run `docker compose -f deploy/compose.yml up`. The configuration is read at start and a bad
variable stops the worker naming it.

## How to test it

`gofmt -l .` must print nothing, then `go vet ./...` and `go test ./...` (Go 1.26). The same run in `ci.yml` on pull requests.

## What is missing

Declared: the routes of the cash api (`/internal/cash-days/open` and `auto-close`) were tested against a stub because they may not be on its `develop` yet; the pending-closure alert is a logging double until the notification piece exists.

## Branching

Three permanent branches. **None of them accepts a direct commit** — you enter through a child
branch and leave through a Pull Request.

```
develop  <--PR--  feat/... fix/... chore/...
qa       <--PR--  qa/...
main     <--PR--  release/...  hotfix/...
```

Promotion happens **by re-application** (`git cherry-pick -x`), never by merging one permanent
branch into another: `merge develop -> qa` and `merge qa -> main` do not exist in this model.

`main` requires **1 approval from `ariel5253`**. On `develop` and `qa` the team sets its own review
rule.

Full policy: `00-governance/branching-policy.md` in `multi-tour-docs`.
