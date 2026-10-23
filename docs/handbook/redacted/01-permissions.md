---
title: "The permission model"
project: redacted
topics: [authorization, rbac, policy-as-data, least-privilege, multi-tenancy, generated-tests, generated-docs]
sources:
  - src/policy/policy.ts
  - src/policy/policy.spec.ts
  - src/common/http.ts
  - src/projects/projects.service.ts
  - scripts/policy-table.ts
  - test/authorization-matrix.e2e-spec.ts
  - src/common/pagination.ts
  - src/auth/authentication.ts
  - test/declared-actions.e2e-spec.ts
  - test/pagination.e2e-spec.ts
verified: 2026-10-09
---

# The permission model

Everything the API allows or refuses comes from one file, `src/policy/policy.ts`: 228 lines with
no database and no framework in them. This page is about that file and why it is shaped the way
it is.

## The words used on this page

- **Authentication:** working out who is making a request. (Part 3.)
- **Authorization:** deciding whether they may do what they're asking. This page.
- **Principal:** whoever is making the request: a signed-in member, or an API key.
- **Role:** a named set of permissions given to a member (`editor`, `viewer`…).
- **Action:** one thing that can be done, named `thing:verb` (`document:update`).
- **Resource:** the thing an action touches.
- **RBAC (role-based access control):** deciding by the principal's role.
- **Tenant:** one customer organization; nothing crosses between two.

## What are the pieces?

Five roles, nineteen actions, three reaches.

- **Roles:** `org_admin`, `department_admin`, `editor`, `viewer`, `auditor`. Three belong to a
  department (`department_admin`, `editor`, `viewer`); the other two act across the organization.
- **Actions:** `department:create`, `member:disable`, `document:share`, `audit:read`… nineteen in
  all.
- **Reach:** how far a permission extends.
  - `org`: anything in the organization;
  - `department`: only in the member's own department;
  - `own`: only what the member created, in their department.

The whole model is one table, `POLICY`: for each action, which roles may do it and how far.

```ts
'document:update': { org_admin: 'org', department_admin: 'department', editor: 'own' },
```

Read it as: an organization admin may update any document; a department admin, those in their
department; an editor, their own. A role that isn't listed can't do it at all. That is **deny by
default**: forgetting to mention a role refuses it.

## How is a decision made?

One function, `can(principal, action, resource)`:

1. A different organization: no. Always first.
2. Look up the reach for this principal and action. None: no.
3. `org`: yes.
4. `department`: yes if the resource is in the principal's department.
5. `own`: yes if it is in their department **and** they created it.

Every endpoint asks it. A service method looks like this (`projects.service.ts`):

```ts
const project = await findProject(db, id);
authorize(principal, 'project:update', resourceOf(project));
```

`authorize` throws "403 Not allowed to project:update" when `can` says no. For lists, where there
is no single resource, `listFilter` returns the restriction to put on the query: the whole
organization, one department, or nothing.

## Why is it a table and not checks in each endpoint?

Because a table can be read, generated from and tested against.

- **Read:** the whole model fits on a screen. A reviewer, an auditor or a customer can check it
  without reading the endpoints.
- **Documented without drift:** the permission table in the README is generated from `POLICY`
  (`scripts/policy-table.ts`), and CI fails if the README is out of date.
- **Tested exhaustively:** the end-to-end suite generates one request for every principal ×
  every action × every relationship (own department, another department, another organization),
  and takes the expected answer from `can()` itself (`authorization-matrix.e2e-spec.ts`). That
  proves each endpoint enforces what the table says. A last test fails if any action has no
  scenario, so a new action can't be added without being tested.

With checks written inside each endpoint, none of the three is possible: the model exists only as
the sum of the code.

## What can't the table say?

Some rules depend on more than role and reach, so they are functions beside the table:

- **Who may give whom a role** (`canAssignRole`): organization admins, anything; department
  admins, only editors and viewers in their own department. **Nobody changes their own role,** so
  nobody promotes themselves and the last admin can't demote themselves by accident.
- **Who may change or disable a member** (`canManageMember`): the same shape, so a department
  admin can't demote a peer.
- **API keys** (`INTEGRATION_ACTIONS`): a key holds explicit scopes, and can only ever be granted
  four actions (read projects; create, read and update documents). Members, keys and the audit
  log stay human-only whatever a key claims.
- **Sharing:** a document can be shared with a person or a department, as reader or editor,
  optionally until a date. A share only ever **raises** access, reaches across departments (that
  is its point), never across organizations, and applies to people, never to keys.
- **Clearance:** a second, independent axis. A member has a clearance level; a section of a
  document has a classification. (Part 5.)

## Why 403 in one case and 404 in another?

Inside an organization, a refused action answers **403**: members know which departments exist,
so "you may not" is honest. Anything in another organization answers **404**: the database makes
it invisible (Part 2), and "forbidden" would confirm that it exists.

## How is the file itself tested?

- **Unit tests** (`policy.spec.ts`), for every function.
- **Mutation testing** (Stryker): the tool changes the code in small ways (`>=` to `>`, `&&` to
  `||`, a `true` to `false`) and runs the tests after each. A change the tests don't notice is a
  gap. On this file the tests catch every meaningful change; two lines are marked as exempt,
  each with the reason (the changed code behaves identically).
- **The generated matrix** above, which tests the endpoints against the file.

## What stops an endpoint forgetting to ask?

The check is a call inside the service method, because it needs the resource loaded first: you
can't ask "may they delete *this* document?" before you have the document. That makes it a line
someone could leave out.

So every route also **declares** the action it needs:

```ts
@Delete('documents/:id')
@Requires('document:delete')
```

Declaring isn't checking. But each request keeps a note of every action the policy was asked
about, and when the handler finishes, the request is held to its declaration:

| The handler finished and… | Result |
|---|---|
| the policy was asked about the declared action | the answer goes out |
| it never was | 500, and everything the request wrote is rolled back |
| the route declares nothing at all | 500, and a test fails before it ships |

A test rewrites the delete method to skip its check and sends a viewer's delete: the answer is
500 and the document is still there. Another test walks every route and fails if one declares no
action, or if the policy has an action no route needs.

What this guarantees is that the policy was *consulted*. That its answer was obeyed is what the
441-case matrix proves.

## How are long lists returned?

A list is a plain array of at most 100 rows. While there is more, the response carries the next
page's address in a `Link` header.

The pages are cut by **keyset**, not by offset: "the rows after this one", not "skip 200 rows".
Two consequences:

- Rows added or removed while someone is paging never shift what they see next.
- A page costs the same however deep it is.

The cursor is the last row's sort value and id. The timestamp is kept to the microsecond, because
a JavaScript date rounds to the millisecond and rows created in one statement would then repeat
or go missing. A test pages through 23 rows that share one timestamp and sees each exactly once.

## Known gaps

- **The policy is code, not configuration.** Changing a permission is a release. An organization
  can't define its own roles; every tenant has the same five.
- **One role and one department per member.** Someone who is an editor in two departments needs
  shares, or two accounts.
- **`own` needs the same department.** An editor moved to another department loses the right to
  edit what they wrote in the old one. That is deliberate, and surprising.
- **Role and reach are the whole vocabulary.** Rules like "only during working hours" or "only
  from the office network" (attribute-based access control) have no place to go.

## Questions and answers

**What is the difference between authentication and authorization?**
Authentication establishes who is asking; authorization decides whether they may. This file does
only the second: it is handed a principal and never asks how it was identified.

**Why "deny by default"?**
A role missing from a row is refused. The mistake that's easy to make (forgetting a role) fails
safe. The opposite design, listing what is forbidden, turns every omission into access.

**Why is the organization check first in `can()`?**
It is the rule with no exceptions, so it comes before anything that could say yes. The database
enforces the same boundary independently (Part 2); this is the second of two locks.

**What's the difference between RBAC and ABAC, and which is this?**
RBAC decides by role. ABAC (attribute-based) decides by properties of the user, the resource and
the situation. This is RBAC with two attributes added where roles weren't enough: who created the
resource (`own`), and clearance against classification.

**Why can't a department admin make someone a department admin?**
They could then create a peer who outranks their own restrictions, or pack a department with
admins. Only the level above hands out a level.

**How do you know the endpoints enforce the table?**
A generated test sends a real request for every principal, action and relationship, and compares
the answer with what `can()` says. Another test fails if an action has no scenario.

**What does mutation testing tell you that coverage doesn't?**
Coverage says a line ran. Mutation testing says a test would notice if the line were wrong.

**How do you make sure a new endpoint checks permissions?**
Each route declares the action it needs. The check itself is in the service, but the request fails
with a 500 and rolls back if the policy was never asked about that action, and a test fails for
any route with no declaration.

**Why keyset paging and not page numbers?**
Page numbers skip rows, so a row added meanwhile shifts every later page and deep pages get slow.
A keyset cursor says "after this row": stable under changes, and the same cost at any depth.
