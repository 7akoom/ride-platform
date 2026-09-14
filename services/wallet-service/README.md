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
- **No automatic debt recovery from a rider's wallet** if a trip is
  disputed or refunded — refunds would be an `adjustment` row written by
  an operator for now.
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
