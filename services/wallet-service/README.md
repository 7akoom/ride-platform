# Wallet Service

Holds rider and driver balances, splits each completed trip between the
driver and the platform, and keeps an immutable ledger of every movement.

## Money is never a float

Every monetary column is `NUMERIC(16,3)` in Postgres and
`decimal.Decimal` in Go. Money crosses the gRPC wire as a **decimal
string** (`"2451.6"`), never a `double`.

This matters more here than anywhere else in the platform. Binary
floating point can't represent most decimal fractions exactly — the
classic `0.1 + 0.2 = 0.30000000000000004`. One calculation, no problem.
A balance that's added to and subtracted from thousands of times over
its life, and the error accumulates until the ledger no longer sums to
the balance, and nobody can say where the money went.

`NUMERIC` and `decimal.Decimal` are exact. And unlike the integer
minor-units approach (storing $12.50 as `1250`), values are stored and
displayed as-is — `2451.6` is literally `2451.6` in the database.

`internal/infrastructure/database/postgres.go` registers the
decimal type handlers on every pgx connection. Without that
registration, pgx wouldn't know how to map `NUMERIC` to
`decimal.Decimal` and money would silently round-trip through float64 —
defeating the whole point.

## The prepaid commission model

Drivers work on a **prepaid balance**: they deposit money with the
platform before they start working, and every trip's commission is drawn
from that deposit. When the balance falls to the suspension threshold,
the account **stops receiving trips entirely** — not just cash ones —
until the driver tops up again.

This is how Careem operates in Iraq, and it's the right model for a
cash-dominant market: the driver physically holds the fare on most
trips, so the deposit is the platform's only real means of collecting
its cut.

A driver's deposit is just `TopUp` with `OWNER_TYPE_DRIVER`. Depositing
enough to cross back above the threshold lifts the suspension **in the
same transaction as the deposit** — there's no window where the money is
in but the account is still blocked, and no background job to run.

Suspension state is recomputed from the balance on every driver movement
(`applyMovementTx`), so the `blocked` flag can never drift out of sync
with the money.

`CheckDriverStanding` is what Dispatch must call before assigning **any**
trip. It returns `amount_due` — exactly what the driver needs to deposit
to get working again — so the driver app can show them a number.

**Settlement is never refused for a suspended driver.** The trip already
happened and the driver is owed their earning. Suspension stops future
assignments; it doesn't retroactively void completed work.

## How a trip settles

The platform takes a commission (default 20%, configurable). Where that
commission comes from depends entirely on how the rider paid:

| Payment | What happens |
|---|---|
| **Wallet** | Rider's balance is debited the fare. Driver is credited `fare − commission`. |
| **Card** | Processor collects the fare. Driver is credited `fare − commission`. Rider's wallet untouched. |
| **Cash** | Driver already holds the **entire fare in hand**, including the platform's commission. So the commission is **drawn from their prepaid balance**. |

That last row is the one that matters in a cash-dominant market. A
driver who takes a 10,000 IQD cash trip has physically collected the
platform's 2,000 commission along with their own 8,000 — the platform
can't reach into their pocket, so it draws the 2,000 from their deposit.

Digital trips work the other way: the driver's earning is *credited*, so
a mix of cash and digital work naturally tops the balance back up.

### Fees of cancelled trips

A cancellation or no-show fee (pricing-service, `fare.calculated` with
`kind`) is settled like a wallet trip whatever the payment method, since no
cash changed hands: the rider's wallet pays what it holds (it never goes
negative), and the rest is recorded on the settlement as `due_amount`,
still owed by the rider. The driver is credited the fee minus the commission
in full.

What a rider owes is collected by the next money that reaches their wallet
(a top-up, a transfer in, change credited, …): inside the same transaction,
under the same wallet lock, the balance pays the oldest fees first, as far as
it goes, each with a `due_payment` ledger row carrying the fee's trip and
`trip_settlements.due_paid` raised by as much. A driver's credit never pays
anything: drivers do not owe fees. `GetRiderDues` (`GET
/v1/wallets/{riderId}/dues`) lists what is still owed; trip-service asks it
(internal token) before a rider requests a trip and refuses them while
`wallet_configs.block_trips_with_dues` is on (the default) and something is
owed. While wallet-service cannot be reached, trips are not blocked.

### The suspension threshold

`suspension_threshold` (default 50,000 IQD) is how far below zero a
balance may fall before the account is suspended. It's a small credit
line past zero — set it to `0` if you want drivers to always be in
credit with no grace at all.

It's also the fraud fail-safe: without it, a driver could run a weekend
of cash trips, pocket the platform's commission on every one, and
abandon the account.

## The ledger is append-only

`wallet_transactions` rows are never updated or deleted. A mistake is
corrected by writing a compensating `adjustment` row, so the full
history of how a balance reached its current value is always
reconstructible. Every row records `balance_after`, so the ledger can be
audited against the wallet without replaying everything.

## Concurrency and idempotency

- Every balance movement locks the wallet row with `SELECT ... FOR
  UPDATE`, so concurrent movements against the same wallet serialize
  instead of racing. This is the single most important correctness
  detail in the service.
- `SettleTrip` is idempotent on `trip_id` (unique constraint). A
  redelivered `trip.completed` event can't pay a driver twice.
- `TopUp` and `RequestPayout` accept an `idempotency_key`, enforced by a
  unique index. A retried top-up can't double-credit.
- All three movements in a settlement (rider debit, driver credit,
  commission) happen in **one transaction** with the settlement record
  and the outbox event. A trip's money is never half-moved.

## Payouts

`RequestPayout` debits the driver's balance and records the intent in
the ledger. It does **not** move money to a bank — that's a separate
concern handled outside this platform (a payment provider, or a manual
transfer by the operator). The ledger row is the instruction and the
audit trail.

## Transfers between riders

A rider sends money from their wallet to another **registered rider**, found
by the phone they sign in with (`POST /v1/wallets/{riderId}/transfers`):

1. the sender's **wallet PIN** is checked by identity-service first (a wrong
   one counts toward its lock; nobody learns whether a phone belongs to a
   rider without the PIN);
2. the phone is looked up in identity-service, then the rider profile in
   rider-service (a person with no rider profile is "not found"); never the
   sender themselves;
3. one transaction locks both wallets (in a fixed order: two riders sending
   to each other at once cannot deadlock), checks the sender's last 24 hours
   against the limits, and writes the transfer, a `transfer_out` row for the
   sender, a `transfer_in` row for the recipient, and
   `wallet.transfer_completed` (outbox, `WALLET_EVENTS` stream), which
   notification-service turns into a push to the recipient.

The sender's `idempotency_key` is required: the same key again returns the
same transfer (nothing checked or moved again); with another amount or phone
it is refused. Limits sit on `wallet_configs` (`transfer_min_amount`,
`transfer_max_amount`, `transfer_daily_amount`, `transfer_daily_count`;
defaults 250 / 1,000,000 / 2,000,000 IQD / 20). Needs
`IDENTITY_SERVICE_ADDRESS`, and identity-service must share
`INTERNAL_SERVICE_TOKEN`.

## Money requests

A rider asks for money (`POST /v1/wallets/{riderId}/money-requests`): from
one registered rider, by phone, or **open** (no payer): anyone with its code
may pay it, which is what a link or a QR code carries. The code is 10
characters without 0/O/1/I/L; a request lives 1-168 hours (72 by default) and
asks for what a transfer may move. A request for a rider writes
`wallet.money_requested` (outbox): that rider gets a push with the code.

Paying it (`…/{code}:pay`, the payer's PIN) is a transfer from the payer to
the requester with the key `request:<id>`, in the transaction that locks the
request row: it is paid once, however many pay at the same time, and paying
again returns the same payment. The one asked may decline it and the
requester cancel it while it is pending; a pending request past its end is
`expired` (never stored: it is computed, and an expired request cannot be
paid or closed).

## Statements

`GET /v1/wallets/{ownerId}/statement` reads the ledger over a period (30 days
by default, at most 366): the balance before it and at its end (the
`balance_after` of the last row before each), what came in and went out, and
the rows newest first, filtered by direction and type and paged. Rows written
in one transaction share `created_at`, so `wallet_transactions.seq` (an
identity column) orders them: a credit, then the fee it paid.

## Vouchers

Prepaid codes the platform sells through a seller (ZainCash, shops); a rider
types one in and its amount reaches their wallet. Staff holding
`vouchers.manage` (only the owner role by default) run them under
`/v1/admin/voucher-batches` and `/v1/admin/vouchers`; every call is
authorized and audited by staff-service.

- **Create** a batch (label, seller, amount, 1-10,000 vouchers, an end at
  least an hour and at most three years ahead, a year by default, and an
  idempotency key). The service generates the codes: 16 symbols without
  0/O/1/I/L (about 79 bits each), shown as `XXXX-XXXX-XXXX-XXXX`, with a serial
  `V<batch number>-<index>` printed next to each. Nothing is redeemable yet.
- **Export** it, once (`…/{batchId}:export`): the codes come back (and as a
  CSV for the seller) and the batch becomes redeemable. The database keeps a
  code sealed (AES-GCM) only until then; after the export only its HMAC
  remains, so neither a second export nor a copy of the database gives the
  codes back. An export that did not reach you is a batch to cancel and
  issue again.
- **Cancel** a batch (with a reason) or **void** one voucher by its serial (a
  card reported lost); vouchers already redeemed stay redeemed. **Look up** a
  voucher by serial: its batch, whether it could be redeemed now, who
  redeemed it and the ledger row.

A rider redeems with `POST /v1/wallets/{riderId}/vouchers:redeem` (spaces,
dashes and case do not matter). The voucher row is locked in the same
transaction as the credit (a `voucher` ledger row with the key
`voucher:<id>`), so it is redeemed once however many try at once; the same
rider again gets the same redemption back. Like any money reaching a rider's
wallet it pays their unpaid trip fees first. Every failed code (unknown, used,
cancelled, expired) is counted: `VOUCHER_REDEEM_MAX_FAILURES` of them within
`VOUCHER_REDEEM_WINDOW` (5 in an hour by default) and the rider waits
(`429`) until the oldest leaves the window; a typo that cannot be a code at
all is refused without counting.

`VOUCHER_CODE_KEY` derives the hash and the seal keys. Outside development it
must be a real secret (32+ characters) and must never change once vouchers
are issued: codes issued under the old key stop working.

## Configuration

`wallet_configs` is versioned like `pricing_configs`: change the
commission rate or suspension threshold by inserting a new row, newest
wins, old rows stay as an audit trail of what past settlements were
calculated under. Migration `00006` seeds 20% commission / 50,000 IQD
suspension threshold / 10,000 IQD minimum payout — tune these once you
see a real cash-to-digital trip mix.

## What's intentionally NOT done yet

Beyond the shared deferred list (observability, auth, tests, NATS
worker):

- **Settlement is called explicitly, not event-driven.** The real design
  is for Wallet to consume `trip.completed` and settle automatically.
  Right now you call `SettleTrip` yourself — same "prove the core logic
  first" approach as the rest of the platform.
- **No card processor integration.** `PAYMENT_METHOD_CARD` assumes the
  fare was collected elsewhere and only handles the internal split.
  Wiring a real processor means picking one that operates in the target
  market.
- **Dispatch doesn't call `CheckDriverStanding` yet.** It should, so
  suspended drivers are filtered out before assignment. Small change to
  dispatch-service, worth doing next.
- **No refunds of a disputed trip yet** — an `adjustment` row written by an
  operator for now (staff adjustments and refunds come with P6d).
- **No admin RPCs** for blocking a wallet or writing adjustments — SQL
  for now.

## Running locally

```bash
cd services/wallet-service
cp .env.example .env   # adjust DATABASE_URL
go mod tidy
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/wallet-service
```

## Trying it

```bash
# Top up a rider
grpcurl -plaintext -import-path ./proto -proto ride/wallet/v1/wallet.proto \
  -d '{"owner_type": "OWNER_TYPE_RIDER", "owner_id": "<rider-id>", "amount": "50000", "idempotency_key": "topup-001"}' \
  localhost:50058 ride.wallet.v1.WalletService/TopUp

# Settle a wallet-paid trip
grpcurl -plaintext -import-path ./proto -proto ride/wallet/v1/wallet.proto \
  -d '{"trip_id": "<trip-id>", "rider_id": "<rider-id>", "driver_id": "<driver-id>", "fare_amount": "4047.9875", "payment_method": "PAYMENT_METHOD_WALLET"}' \
  localhost:50058 ride.wallet.v1.WalletService/SettleTrip

# Settle a CASH trip — watch the driver's balance go negative
grpcurl -plaintext -import-path ./proto -proto ride/wallet/v1/wallet.proto \
  -d '{"trip_id": "<another-trip-id>", "rider_id": "<rider-id>", "driver_id": "<driver-id>", "fare_amount": "10000", "payment_method": "PAYMENT_METHOD_CASH"}' \
  localhost:50058 ride.wallet.v1.WalletService/SettleTrip

# Deposit against a driver's prepaid commission balance
grpcurl -plaintext -import-path ./proto -proto ride/wallet/v1/wallet.proto \
  -d '{"owner_type": "OWNER_TYPE_DRIVER", "owner_id": "<driver-id>", "amount": "100000", "idempotency_key": "deposit-001"}' \
  localhost:50058 ride.wallet.v1.WalletService/TopUp

# Check whether a driver should be given work at all
grpcurl -plaintext -import-path ./proto -proto ride/wallet/v1/wallet.proto \
  -d '{"driver_id": "<driver-id>"}' \
  localhost:50058 ride.wallet.v1.WalletService/CheckDriverStanding
```
