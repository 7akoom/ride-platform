# Support Service

The support desk. Riders and drivers open tickets from the apps; support staff
work them from the Admin web.

- A ticket belongs to the person who opened it, as a rider or as a driver. It
  has a category, and may name one of their trips (from the last
  `SUPPORT_TRIP_MAX_AGE`) or a wallet transaction.
- Messages go back and forth. A staff reply reaches the person as a push
  (`support.reply_received`); the push never carries the message. Staff can
  also write internal notes nobody else sees. Attachments are
  `SUPPORT_ATTACHMENT` uploads from media-service (up to 5 per message), held
  while the ticket keeps them.
- Opening a second ticket in the same category about the same trip adds the
  message to the open one instead.
- One person may have `SUPPORT_MAX_OPEN_TICKETS` open tickets per audience.

## Statuses

`open` (waiting for staff) → `in_progress` (claimed) → `waiting_user` (a staff
reply waits for the person) → `resolved` → `closed`. A message from the person
reopens a resolved or waiting ticket. The person can close their ticket; a
closed ticket is final.

## Special tickets

- **Lost items** (`lost_item`): needs a completed trip with a driver. The
  trip's driver is brought in: told (`support.lost_item_reported`), sees the
  ticket among their own, and answers inside it. Neither sees the other's
  phone, and the driver does not see the rider's transaction.
- **Safety** (`safety`, `accident`, and `sos`): always urgent, out of the normal
  queue, only for staff with `support.safety`. The other side of the trip never
  sees them.
- **SOS**: every `trip.sos_triggered` event (durable consumer
  `support-trip-sos` on `TRIP_EVENTS`) opens a safety ticket, once per alert, in
  the name of whoever pressed it, with the position as a system note. Operator
  alerts by SMS/webhook stay in notification-service.

## Actions from a ticket

| Action | Permission | What it does |
|---|---|---|
| `REFUND` | `support.refund` | wallet `RefundTrip` for the ticket's trip (rider tickets); `driver_amount` is taken back from the driver |
| `WAIVE_FEE` | `support.refund` | refunds what is left of a cancelled trip's fee (an unpaid fee is waived) |
| `COMPENSATION` | `support.refund` | credits the wallet of the person who opened the ticket |
| `SUSPEND_ACCOUNT` | `support.suspend` | identity suspends the requester or the other side of the trip (sessions revoked, sign-in refused); a driver is taken offline; optional `suspend_until` (1 h – 90 days) lifts it automatically |
| `REACTIVATE_ACCOUNT` | `support.suspend` | lifts a suspension |

Money a ticket moves beyond `SUPPORT_REFUND_LIMIT` waits (`PENDING_APPROVAL`)
for someone else with `support.approve`. Wallet calls are made as the staff
member who asked (or approved), recorded by wallet-service as theirs, with the
idempotency key `support-action:<id>`. A refusal fails the action with the
reason; a service that does not answer leaves it `PROCESSING`, and a worker
(`SUPPORT_WORKER_INTERVAL`) tries again with the same key until it answers.

## Who may do what

People: `ListSupportCategories`, `CreateTicket`, `ListMyTickets`,
`GetMyTicket`, `AddTicketMessage`, `CloseMyTicket`, `GetAttachmentURL`
(`/v1/support/...`), only on their own tickets.

Staff (`/v1/admin/support/...`, every call audited by staff-service):

| Permission | |
|---|---|
| `support.read` | queue, ticket with notes and actions (safety tickets also need `support.safety`) |
| `support.reply` | claim, reply, internal notes, status, priority |
| `support.manage` | assign to another staff member |
| `support.refund` | refund, fee waiver, wallet credit up to the limit |
| `support.approve` | the approval queue: approve or reject money asked by someone else |
| `support.suspend` | suspend and reactivate accounts |
| `support.safety` | the safety queue and safety tickets |
| `support.configure` | categories |

System roles (staff-service migration 00004): **Support agent** (read, reply,
refund) and **Support lead** (all but configure). The owner holds everything.

The internal service token is refused here: no other service calls this one.

## Events (`SUPPORT_EVENTS`, subjects `support.>`)

`support.ticket_created`, `support.reply_received`, `support.ticket_resolved`,
`support.lost_item_reported`. The last three carry `recipient_type`
(`rider`/`driver`) and `recipient_id`; notification-service turns them into
pushes (templates in its migration 00014).

## Running

```bash
cp .env.example .env
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/support-service
```

Repository and service tests against PostgreSQL:

```bash
SUPPORT_TEST_DATABASE_URL=postgres://...  go test ./...
```

End to end: `bash scripts/e2e/test-support.sh`.
