---
title: "Signing in: passwords, tokens and API keys"
project: redacted
topics: [authentication, argon2, jwt, refresh-token-rotation, api-keys, account-lockout, timing-attacks, rate-limiting]
sources:
  - src/auth/auth.service.ts
  - src/auth/tokens.ts
  - src/auth/passwords.ts
  - src/auth/authentication.ts
  - src/auth/auth.controller.ts
  - src/api-keys/api-keys.service.ts
  - src/common/crypto.ts
  - src/app.module.ts
  - test/auth.e2e-spec.ts
  - test/tokens.e2e-spec.ts
verified: 2026-10-08
---

# Signing in: passwords, tokens and API keys

[Part 1](01-permissions.md) decides what a principal may do. This page is about how the API learns
who the principal is: people sign in with a password and carry tokens; machines carry an API key.

## The words used on this page

- **Hash:** a one-way fingerprint of a value: easy to compute, impractical to reverse.
- **Access token:** a short-lived proof of identity sent with every request.
- **Refresh token:** a longer-lived secret used only to get a new access token.
- **JWT (JSON Web Token):** a small signed document. Anyone can read it; only the holder of the
  signing key can produce one that verifies.
- **Rotation:** replacing a secret with a new one each time it is used.
- **Timing attack:** learning a secret from how long an operation takes.
- **Enumeration:** learning which accounts exist from how the system answers.

## How is a password stored?

It isn't; its hash is, made with **Argon2id** (`src/auth/passwords.ts`).

Passwords are short and guessable, so the hash must be deliberately expensive: each guess should
cost an attacker real time and memory. Argon2id is built for that, and the settings are the
minimum OWASP recommends (19 MiB of memory, two passes). A password must be 12 to 128 characters;
there are no "one capital, one symbol" rules, following NIST's guidance that length matters and
composition rules don't.

## What happens at sign-in?

`login` in `src/auth/auth.service.ts`:

1. Ask the database which member and organization an email belongs to (one of the functions from
   [Part 2](02-tenants-and-database.md); it returns two ids).
2. Inside that organization's transaction, lock the member's row and check the password.
3. On success: reset the failure count, record `auth.login`, issue a token pair.
4. On a wrong password: count it. The fifth in a row locks the account for 15 minutes.

Three details are deliberate:

- **One answer for every failure.** An unknown email, a wrong password, a locked account and a
  disabled one all get the same 401, "Invalid email or password". The response never says which,
  so it can't be used to find out which emails have accounts.
- **About the same time, too.** For an unknown email there is no hash to check, which would make
  that case measurably faster. So a dummy hash is verified instead, and the miss costs as much
  time as a wrong password.
- **A failed attempt is still recorded.** The request ends in an error, and a request that throws
  rolls back its transaction ([Part 2](02-tenants-and-database.md)). So `login` doesn't throw
  inside the transaction: it returns "failed" as a value, lets the transaction commit the failure
  count and the audit event, and only then raises the error.

## What does a signed-in member carry?

Two things, with different jobs:

| | Access token | Refresh token |
|---|---|---|
| What it is | A JWT, signed with HS256 | 32 random bytes |
| Lives | 15 minutes | 30 days, but each one works **once** |
| Sent | With every request, as `Authorization: Bearer …` | Only to `/auth/refresh` |
| Stored by the server | Not at all | As a SHA-256 hash |
| Says | Who you are and which organization | Nothing: it is a random handle |

**The access token says who you are, never what you may do.** It holds a member id and an
organization id. The role, department and clearance are read from the database on every request
([Part 2](02-tenants-and-database.md)), so a demotion or a disabled account takes effect at once,
not when a token expires. This removes the usual weakness of JWTs, that they can't be revoked.

**Then why a JWT at all?** Because of the organization. Every query needs the organization set
before it can see a row, and the signed token supplies it without a database lookup: the
signature proves the organization id wasn't forged. A random session id would need a
cross-tenant lookup on every request to learn it.

When the token is verified (`verifyAccessToken`), only HS256 is accepted, and the issuer,
audience, token type and the shape of both ids are checked. Naming the algorithm matters: a
verifier that accepts whatever the token claims can be handed one that says "none".

## How do refresh tokens rotate, and what does rotation catch?

Every use of a refresh token returns a new pair and marks the old token as used. All the tokens
descended from one sign-in share a **family**.

If a token that was already used turns up again, two parties hold it: the real client, and
someone who copied it. The server can't tell which is which, so it **revokes the whole family**
and records `auth.refresh_reuse_detected`. Both are signed out; the real member signs in again;
the thief's copy is dead.

Without rotation, a stolen refresh token works quietly for 30 days. With it, theft is noticed the
first time both parties use it.

Signing out revokes the family. Disabling a member revokes all their tokens.

## How do machines authenticate?

With an API key, `rbac_<prefix>_<secret>`, sent in its own header (`X-API-Key`):

- The **prefix** (12 hex characters) is stored in the clear and used to find the key.
- The **secret** (32 random bytes) is stored only as a SHA-256 hash, and compared in constant
  time, so the comparison's duration reveals nothing.
- The full key is shown **once**, when it is created.
- A key holds explicit scopes, may be bound to one department, may expire, and can be revoked.

**Why SHA-256 here and Argon2 for passwords?** A slow hash protects secrets that can be guessed.
A key's secret is 256 random bits: there is nothing to guess, so a fast hash is correct, and it
keeps every request from an integration cheap.

A request may carry a bearer token or an API key, never both, and each is accepted only in its own
header, so one can't be replayed as the other.

## What slows down guessing?

- **A rate limit** per client address: 10 requests a minute on sign-in, sign-up and refresh; 300 a
  minute elsewhere.
- **Lockout:** five wrong passwords lock an account for 15 minutes.
- **The cost of each guess:** Argon2id.

Lockout has a known price: anyone can lock someone else out by guessing wrongly on purpose. The
lock is short, the rate limit slows the attempt first, and the response never reveals that an
account is locked.

## Known gaps

- **Sign-up reveals which emails are registered.** It answers 409, "That email is already
  registered", which is exactly the enumeration that sign-in is careful to avoid.
- **Tokens are returned in the response body,** so the client decides where to keep them. The
  demo keeps them in the browser's session storage, where a script injected into the page could
  read them. A cookie the page can't read (as Lighthouse uses) is safer for a browser; a body is
  what a programmatic client needs. This API chose the second.
- **One signing secret, with no way to rotate it gracefully.** Changing `JWT_SECRET` invalidates
  every access token at once. Several services verifying tokens would call for asymmetric keys.
- **Used and expired refresh tokens are never removed.** The application can't delete from that
  table, and nothing else does, so it grows by one row per refresh.
- **Two tabs refreshing at the same moment look like theft.** The second presents a token the
  first just used, and the family is revoked: the member is signed out.
- **The rate limit is counted in memory,** per instance, and resets when the container stops.
- **No second factor, no password reset, no email verification.** Sign-up is open to anyone.

## Questions and answers

**Why Argon2id rather than SHA-256 for passwords?**
SHA-256 is fast, which is what an attacker guessing passwords wants. Argon2id is deliberately slow
and memory-hungry, so each guess is expensive.

**If the role isn't in the token, what is the token for?**
Identity, and the organization, without a database lookup: the signature proves the organization
id is genuine, and that id is what scopes the transaction in which the role is then read.

**A member is disabled while holding a valid access token. What happens?**
Their next request fails with 401: loading the principal finds no enabled member. Their refresh
tokens are revoked too.

**What does refresh-token rotation detect?**
A copied token. Each token works once, so a second use means two parties hold it, and every token
from that sign-in is revoked.

**Why does sign-in answer the same way for an unknown email and a wrong password?**
So the answer can't be used to learn which emails have accounts. The timing is equalised for the
same reason.

**How does a failed sign-in get recorded if the request fails?**
The handler returns the failure as a value so the transaction commits the failure count and the
audit event, then raises the error outside it.

**Why can an API key be hashed with SHA-256?**
Its secret is 256 random bits. Slow hashes exist to protect guessable secrets; this one isn't.

**Why are a token and a key never both accepted?**
Each has one header and one meaning. Accepting both would leave a choice about which wins, and
choices in authentication are where bugs live.
