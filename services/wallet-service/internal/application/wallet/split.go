package wallet

// SplitWalletPayment divides a fare the rider chose to pay from their wallet
// into the part the wallet can cover and the part the rider hands to the
// driver in cash. The wallet part never exceeds the balance, so a wallet trip
// can never fail for lack of funds: whatever the wallet cannot cover is cash.
// A missing or empty wallet simply means the whole fare is cash.
func SplitWalletPayment(fare, balance Money) (walletPart, cashPart Money) {
	zero := fare.Sub(fare)

	switch {
	case !fare.IsPositive():
		return zero, zero
	case !balance.IsPositive():
		return zero, fare
	case balance.GreaterThanOrEqual(fare):
		return fare, zero
	default:
		return balance, fare.Sub(balance)
	}
}

// DriverSettlementAmount is the signed movement on the driver's wallet for a
// trip whose fare was paid partly from the rider's wallet. The platform holds
// the wallet part; the driver holds the cash part; the driver is entitled to
// the fare minus the commission. So the platform owes the driver
// (walletPart - commission): a credit when the wallet part is larger than the
// commission, a debit (a commission drawn from the prepaid balance) when it
// is smaller. Full wallet payment gives fare - commission, full cash gives
// -commission, exactly as before.
func DriverSettlementAmount(walletPart, commission Money) Money {
	return walletPart.Sub(commission)
}
