# Build list from the review of offline-driver

Nothing is built during the review. Sizes: S (under an hour), M (a few hours), L (a day or more).

| # | What | Why | Size |
|---|---|---|---|
| O1 | **Make repeats safe by default:** pass each action's id to its handler as a ready-made idempotency key, document it prominently, and show it in the README's example. | A request whose answer was lost is retried. Without an id the server can recognise, that can create a second order. | S |
| O2 | **Let actions depend on each other:** when an action is dead-lettered, hold back (or dead-letter) the queued actions that name the same record. | "Edit that order" is still sent after "create that order" has failed for good. | M |
| O3 | **Let merging forget what the server deleted:** accept the ids a page says are gone, or a full-refresh mode that replaces instead of merging. | Saved items that a fetched page doesn't mention are kept for ever. | M |
| O4 | **Say how old offline data is:** return "as of" with a cached read. | Old data is returned exactly like fresh data. | S |
| O5 | **Wait longer between retries of the same action** (a growing delay). | A failed action is retried at the next reconnect or new action, however soon. | S |
| O6 | **Publish to the package registry.** | It is installed from the repository today. | S |
