# Add OAuth refresh handling

Add OAuth refresh handling to the GitHub provider. The `RefreshToken`
method on `GitHubProvider` should retry on expired tokens and update the
stored credentials in `internal/store/token_store.go`.

Include unit tests covering the refresh path and update `docs/auth.md`
with the new flow.
