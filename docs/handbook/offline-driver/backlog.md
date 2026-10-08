# Build list from the review of offline-driver

Nothing is built during the review. Sizes: S (under an hour), M (a few hours), L (a day or more).

| # | What | Why | Size |
|---|---|---|---|
| O1 | **Make repeats safe by default:** pass each action's id to its handler as a ready-made idempotency key, document it prominently, and show it in the README's example. | A request whose answer was lost is retried. Without an id the server can recognise, that can create a second order. | S |
| O2 | **Let actions depend on each other** (design by Martin, 2026-10-09): when registering handlers, declare that one action type relates to another by a field (`edit-order` relates to `create-order` by order id). Before running an action, the driver looks for a related earlier action still in the queue: if it is stuck, the dependent is held (and dead-lettered with it if discarded); if it is absent, it succeeded, so the dependent runs. No history of past actions is needed, and nothing has to be remembered at each call site. Also needed: replacing a locally made id with the server's once the create succeeds. | "Edit that order" is still sent after "create that order" has failed for good. | M |
| O3 | **Let merging forget what the server deleted:** accept the ids a page says are gone, or a full-refresh mode that replaces instead of merging. | Saved items that a fetched page doesn't mention are kept for ever. | M |
| O4 | **Say how old offline data is:** return "as of" with a cached read. | Old data is returned exactly like fresh data. | S |
| O5 | **Wait longer between retries of the same action** (a growing delay). | A failed action is retried at the next reconnect or new action, however soon. | S |
| O6 | **Publish to the package registry.** | It is installed from the repository today. | S |
| O7 | **A helper for large first loads** (Martin, 2026-10-09): write rows in small transactions, give the screen a turn between them, and report progress. | The database layer is synchronous, so one big write freezes the screen; the advice to split it up is in a comment, with nothing to do it. | S |
| O8 | **A time limit on each action's attempt.** | A handler whose request never answers keeps the drain "running" for ever, and nothing else is sent. | S |
| O9 | **Set a corrupted queue aside instead of deleting it,** and report it. | A queue that can't be read is cleared so new work isn't blocked; whatever it held is lost without a chance to recover it. | S |
