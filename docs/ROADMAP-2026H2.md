# Roadmap 2026-H2 — claw-code-go

> Produced by an iterion `whats-next` (Nexie v3) roadmap study on
> 2026-07-16 at HEAD `c9eed79` — 3 parallel read-only audits (docs/ADR,
> code-gaps, operational-state) + operator arbitrage (axis:
> **reliability/validation**). Runs `019f6b46` (study) — full method in
> iterion's `docs/bot-runs/whats-next.md`. Central tension: the scope is
> functionally finished (parity 32/33 COMPLETE, ~0 TODO) but nothing
> proves it — the README self-qualifies as *"Experimental — most
> features not manually validated"*. This half is about **proof**, not
> features.

## Chantiers

| Tier | Chantier | Owner suggestion |
|---|---|---|
| **NOW** | **C1** — CI (build+vet+test on push/PR) ← *initial ready lot* | feature-dev |
| **NOW** | **C2** — Validation runbook + binary E2E smoke test | feature-dev |
| NEXT | **C3** — Tests for the blind zones: `cmd/`, **`internal/auth`** (5 src, 0 test), `internal/usage` — not the `pkg/api/*` façades; 18/61 packages have no test, 37 `t.Skip` | test-coverage (Testy) |
| NEXT | **C4** — Validate the cloud providers (`live` build-tag tests never run) | manual / needs creds |
| NEXT | **C5** — Reconcile deferred debt vs code — 2 open P1s: F-CC-1 (OpenAI `/responses` identity loss), F-CC-4 (`anthropic-beta` header on OAuth); CHANGELOG says "0 deferred" while `docs/reviews/iterion-touching-audit-2026-05-17.md:60-69` lists 9 findings | verify → improve-loop |
| LATER | **C6** — ADR backfill (no `docs/adr/`, decisions scattered) | adr-cartograph (Adry) |
| LATER | **C7** — Workflow DSL "v2 seams" (schema returns, budgets, resume — `parity.md:44`) | feature-gap-fill (Fini) |
| LATER | **C8** — Harden never-exercised surfaces (computer-use, sandbox namespaces, cosign plugins, OAuth MCP) | whole-improve-loop |
| LATER | **C9** — Release/distribution: single stale tag `v0.1.0` (2026-05-07, ~110 commits behind), no `go install` path, no Dockerfile/goreleaser, README "Quick start" is a lib snippet | feature-dev |

**Quick-wins**: refresh `docs/parity.md` (frozen at 2026-04-28) + finish
the truncated contributor section (`parity.md:47-52`) → docs-refresh ·
fix F-CC-8 audit state (shipped, still marked deferred) → docs-refresh ·
align toolchain `go.mod go 1.25.0` ↔ devbox `go@1.26`.

**Dep-hygiene note (later)**: 21 direct deps — AWS+Azure+GCP SDKs are
pulled even for Anthropic-only use.

## Framed tickets (ready to paste on a board)

### C1 — CI GitHub Actions: build + vet + test
`labels: source:whats-next, horizon:now, axis:reliability` · `bot: feature-dev`

```markdown
## Context
Aucun `.github/workflows/` n'existe — chaque garantie ("compiles and passes go vet",
README:21,302) est manuelle, rien n'empêche une régression de lander sur `main`.
Trou de fiabilité n°1 (étude roadmap 2026-07-16, HEAD c9eed79). Les commandes qualité
existent déjà comme scripts devbox (`devbox.json:9-12` build/test/check) : la CI les
exécute, ne les réinvente pas.

## Done criteria
- `.github/workflows/ci.yml` s'exécute sur `push` et `pull_request`.
- Il lance `go build ./...`, `go vet ./...`, `go test ./...` sur go 1.25+.
- Le toolchain CI est aligné sur `go.mod` (`go 1.25.0`) — pas de drift.
- Les tests `live` (build-tag) NE sont PAS requis pour passer (ils skippent sans creds).
- Le job est vert sur `main` au commit de landing.

## Verify
- `ls .github/workflows/ci.yml` existe.
- `grep -E 'go (build|vet|test)' .github/workflows/ci.yml` retourne les 3 étapes.
- L'onglet Actions montre un run vert sur le dernier push `main`.
- Un PR qui casse volontairement un test fait passer la CI au rouge.
```

### C2 — Runbook de validation + smoke test E2E du binaire
`labels: source:whats-next, horizon:now, axis:testing` · `bot: feature-dev`

```markdown
## Context
Le README s'auto-qualifie "Experimental — most features not manually validated"
(README:21,302) sans matrice de validation en face. `cmd/claw-code-go` a 0 test et
aucun test ne lance le binaire de bout en bout. Étude roadmap 2026-07-16.

## Done criteria
- Un test E2E (build-tag `e2e` ou script) construit le binaire et exécute un vrai
  aller-retour `--prompt "…" → réponse` sur ≥1 provider validé (Anthropic ou OpenAI),
  en skippant proprement sans creds.
- Un `docs/validation.md` liste, feature par feature, l'état : "éprouvé E2E" /
  "testé unitairement" / "non validé" — au minimum : model loop Anthropic+OpenAI,
  tools built-in, MCP, computer-use (deps xdotool/X11), sandbox, providers cloud.
- Chaque ligne pointe la commande exacte de reproduction.
- Le chemin "prompt simple → texte" est marqué "éprouvé E2E" avec preuve (sortie citée).

## Verify
- `go test -tags e2e ./... -run E2E` (avec creds) produit ≥1 delta texte + un `message_stop`.
- Sans creds, `go test -tags e2e ./...` skippe sans échec.
- `grep -c 'éprouvé E2E' docs/validation.md` ≥ 1.
```

### Quick-win — Rafraîchir docs/parity.md + compléter le guide contributeur
`labels: source:whats-next, horizon:now, axis:docs` · `bot: docs-refresh`

```markdown
## Context
`docs/parity.md:3` porte un snapshot figé au 2026-04-28 ; ~30 commits ont landé depuis
(workflow goja, subagents réels, semantic_search, oracle, prompt caching). La section
"Quick guidance for contributors" se termine en pleine phrase (`parity.md:47-52` :
"If you're chasing a "PARTIAL" rating to "COMPLETE":" puis EOF). Étude roadmap 2026-07-16.

## Done criteria
- Le snapshot en tête reflète le HEAD courant (date ≥ 2026-07 + note de rafraîchissement).
- La matrice inclut les capacités landées depuis 2026-04-28 avec leur rating.
- La section "PARTIAL → COMPLETE" est complète (plus de phrase tronquée en fin de fichier).
- Aucune ligne ne contredit le CHANGELOG [Unreleased].

## Verify
- `tail -5 docs/parity.md` ne se termine plus sur une phrase coupée.
- `grep -Ec 'semantic_search|workflow|oracle|caching' docs/parity.md` > 0.
- La date de snapshot (`parity.md:3`) est ≥ 2026-07.
```

### Quick-win — Corriger l'état de F-CC-8 dans l'audit iterion-touching
`labels: source:whats-next, horizon:now, axis:docs` · `bot: docs-refresh`

```markdown
## Context
`docs/reviews/iterion-touching-audit-2026-05-17.md:66` marque F-CC-8 (compaction
continuation role=system) "deferred", alors que le CHANGELOG le déclare shippé
(`CHANGELOG.md:55`). Le CHANGELOG affiche par ailleurs "0 deferred" (`CHANGELOG.md:71`)
en contradiction avec les 9 findings de cet audit. Ce quick-win ne traite QUE la mise à
jour documentaire ; le fond (vérifier les 2 P1 restants F-CC-1/F-CC-4 dans le code) est C5.

## Done criteria
- La ligne F-CC-8 reflète son état réel (shippé) avec le sha du fix.
- L'entête de l'audit note la reconciliation et renvoie à C5 pour les P1 restants.
- Plus de contradiction "0 deferred" vs table de l'audit sur F-CC-8.

## Verify
- `grep -n 'F-CC-8' docs/reviews/iterion-touching-audit-2026-05-17.md` ne montre plus "deferred".
- La ligne cite un sha (`git log --oneline | grep -i compact` pour le retrouver).
```

---

**Initial ready lot = C1 alone** — the CI conditions the value of
everything after it. Blind spots the study did not cover: security (no
scanner run), perf/cost (`bench/` unexplored), GDPR/DR (out of scope
for a library).
