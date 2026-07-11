# claw-code-go

Native multi-provider LLM client (Anthropic, OpenAI, Bedrock, Vertex,
Foundry). Public repo, MIT. Standard Go layout; tests with the stdlib
`testing` package.

## Downstream consumer: iterion

iterion (sibling repo — this checkout usually lives at
`<iterion>/.works/claw-code-go`) **vendors** this module and pins it by
pseudo-version in its `go.mod`.

**After landing a change here that iterion needs, bump the pin ONLY with
iterion's [`scripts/bump-claw.sh`](../../scripts/bump-claw.sh)** (run from
the iterion repo root; it pushes the claw commit if needed, then
`go get github.com/SocialGouv/claw-code-go@<sha>` + tidy + vendor +
verify + commit).

**NEVER hand-write the pseudo-version** in iterion's go.mod (nor compute
it with `date`): a timestamp stamped in local time instead of the
commit's UTC time fails `go mod verify` with "does not match
version-control timestamp", which turns iterion's vendor-check red on
main and on every PR merge-ref. `go get` computes the canonical UTC
pseudo-version; the script exists so nobody has to.

Also remember: the pin must be **resolvable** — the pinned commit has to
be pushed to `origin/master` before iterion's CI can verify it (the
script handles that too).
