# Staff Service

Owns the people who run the platform (operations, finance, support), what
each of them may do, and the audit log of what they did.

Staff sign in like everyone else, through identity-service (an email OTP).
Identity proves who they are; the roles and permissions here decide what they
may do. Identity knows nothing about roles.

## How other services use it

An admin RPC in any service is classified `accessStaff` with a permission
(for example `drivers.approve`). Before it runs, the service calls
`AuthorizeStaffAction` with the caller's identity, the permission, the method
and the id it touches. staff-service records the attempt (allowed or denied)
before it answers, so every staff action is in the audit log even if the
service crashes right after. When the action ends, the service reports the
gRPC code with `CompleteStaffAction`.

It fails closed: if staff-service does not answer, the admin RPC answers
`Unavailable` and does not run. The internal service token still bypasses it
(ops scripts, other services), exactly as before.

## Rules

- Permissions are a fixed catalog in `internal/application/staff/permissions.go`.
  A permission is added there, and granted to system roles in a migration, in
  the same change that starts enforcing it.
- System roles (`owner`, `operations`) come from migrations and never change
  through the API. The owner role holds every permission, including ones added
  later. Custom roles can be created, changed and deleted.
- Nobody grants, removes or builds a role with a permission they do not hold
  themselves. Only an owner manages owners or hands out the owner role.
- Nobody suspends themselves or changes their own roles.
- The last active owner can be neither suspended nor demoted (checked in the
  same transaction, serialized on the owner role's row).
- A suspended staff member holds no permission at all, at once.
- The audit log is append-only: a database trigger refuses deletes and any
  change except completing a pending entry once.

## First owner

Set `STAFF_BOOTSTRAP_OWNER_EMAIL` in `.env`. When there is no staff at all,
that address is invited as owner at startup. Sign in with that email (email
OTP through the gateway), then `POST /v1/staff/me:accept`. From then on the
owner invites everyone else (`POST /v1/admin/staff`).

## HTTP routes (through the gateway)

| Route | Needs |
|---|---|
| `GET /v1/staff/me`, `POST /v1/staff/me:accept` | a signed-in user |
| `GET /v1/admin/staff`, `GET /v1/admin/staff/{id}`, `GET /v1/admin/roles`, `GET /v1/admin/permissions` | `staff.read` |
| `POST /v1/admin/staff`, `PUT /v1/admin/staff/{id}/roles`, `POST /v1/admin/staff/{id}:suspend`, `:reactivate`, `:revoke` | `staff.manage` |
| `POST /v1/admin/roles`, `PATCH /v1/admin/roles/{id}`, `DELETE /v1/admin/roles/{id}` | `roles.manage` |
| `GET /v1/admin/audit` | `audit.read` |

`AuthorizeStaffAction` and `CompleteStaffAction` are internal only (no route,
internal token required).

## Running locally

```bash
cd services/staff-service
cp .env.example .env   # set DATABASE_URL and STAFF_BOOTSTRAP_OWNER_EMAIL
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/staff-service
```

Repository tests run against a throw-away database:

```bash
STAFF_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
```
