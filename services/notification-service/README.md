# Notification Service

Renders messages in the recipient's language and fans them out across
in-app, push, and SMS.

## Multilingual by design, not as an afterthought

Templates live in two tables: the event (`notification_templates`) and
its translations (`notification_template_translations`). Adding a
language is **inserting rows**, not a migration — which matters when a
deployment lands in a market whose language the original build never
anticipated.

Migration `00005` seeds the core trip events in **English, Arabic, and
Kurdish Sorani**.

Locale resolution walks a preference chain: the locale the caller asked
for → the locale on the recipient's most recently seen device → `en`.
The first translation that exists wins, resolved in a single query.

Bodies use `{placeholder}` substitution. A variable with no value is
**left as-is** rather than blanked — a template referencing
`{driver_name}` with no driver name should read obviously broken in
testing, not quietly ship "  is on the way" to a real user.

## Channels are pluggable — and one of them can't be self-hosted

Reaching an Android or iOS device means going through **FCM** or
**APNs**. Google and Apple control those channels; there is no
self-hostable alternative. Both are free at any volume this platform
will see.

So the design keeps that dependency behind an interface
(`PushSender`, `SMSSender`). A deployment swaps the implementation
without the rest of the service knowing, and the included FCM client
talks to the HTTP v1 API directly rather than pulling in the Firebase
Admin SDK — that's a large dependency tree for what amounts to one
signed JWT and one POST per device.

**Push is optional.** With no `FCM_CREDENTIALS_FILE` set, the service
starts anyway, logs a warning, and records push deliveries as
unconfigured. In-app notifications keep working. A missing credential
degrades one channel; it doesn't take the service down.

**SMS ships as a no-op.** Gateways are strictly regional — an Iraqi
deployment and a Gulf one use different providers — so baking one in
would help nobody. Implement `SMSSender` against the local gateway when
a deployment has one.

Both no-op senders **report failure honestly** rather than pretending to
succeed. A delivery row saying "not configured" is useful; one saying
"sent" when nothing was sent costs hours to debug later.

### Setting up FCM

1. Firebase console → Project settings → Service accounts → Generate new
   private key.
2. Save the JSON somewhere the service can read it.
3. Point `FCM_CREDENTIALS_FILE` at it and restart.

Dead tokens (app uninstalled, token rotated) come back from FCM as
404/400 and are **pruned automatically**, so they stop being retried
forever.

## Delivery ordering and why failures aren't errors

The in-app record is written first, and its success is what the caller
gets back. Push and SMS are attempted after, and their failures are
recorded as **delivery rows**, not returned as errors.

A notification the user can see in the app is a delivered notification
even if Google's push service was down. Failing the whole call would
tempt callers into retrying — producing duplicates.

Rendered title and body are **stored**, not re-rendered on read: a
notification should always show what the user was actually told, even
after the template is edited or the variables it referenced are gone.

## Devices

A push token is unique **globally**, not per user. When someone signs
out and a colleague signs in on the same handset, `RegisterDevice`
reassigns the token to the new owner rather than rejecting it —
notifications must follow the account, not the device's history.

## Safety detail

`MarkAsRead` scopes every update by recipient, even when explicit IDs
are passed, so a caller can't mark someone else's notifications read by
guessing an ID.

## What's intentionally NOT done yet

Beyond the shared deferred list (observability, auth, tests, NATS
worker):

- **Not event-driven yet.** The real design is for this service to
  consume `trip.*`, `fare.calculated` and `trip.settled` and notify
  automatically. Right now callers invoke `Send` explicitly.
- **No APNs client.** Android works via FCM today. FCM can also relay to
  iOS if you configure an APNs key in Firebase, which is the simpler
  path than a second client here.
- **No per-user channel preferences** ("don't SMS me") — the template's
  default channels apply to everyone.
- **No scheduling or batching** — sends are immediate and one at a time.

## Running locally

```bash
cd services/notification-service
cp .env.example .env   # adjust DATABASE_URL; FCM optional
go mod tidy
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/notification-service
```

## Trying it

```bash
# Send in Arabic (no device registered — push reports as skipped)
grpcurl -plaintext -import-path ./proto -proto ride/notification/v1/notification.proto \
  -d '{"recipient_type": "RECIPIENT_TYPE_RIDER", "recipient_id": "<rider-id>", "event_key": "trip.driver_assigned", "locale": "ar", "variables": {"driver_name": "علي", "vehicle_model": "كورولا", "vehicle_color": "أبيض", "plate_number": "ABC123"}}' \
  localhost:50059 ride.notification.v1.NotificationService/Send

# Same event in Kurdish
grpcurl -plaintext -import-path ./proto -proto ride/notification/v1/notification.proto \
  -d '{"recipient_type": "RECIPIENT_TYPE_RIDER", "recipient_id": "<rider-id>", "event_key": "trip.driver_assigned", "locale": "ku", "variables": {"driver_name": "ئەلی", "vehicle_model": "کۆرۆلا", "vehicle_color": "سپی", "plate_number": "ABC123"}}' \
  localhost:50059 ride.notification.v1.NotificationService/Send

# Read the inbox
grpcurl -plaintext -import-path ./proto -proto ride/notification/v1/notification.proto \
  -d '{"recipient_type": "RECIPIENT_TYPE_RIDER", "recipient_id": "<rider-id>"}' \
  localhost:50059 ride.notification.v1.NotificationService/ListNotifications
```
