/// What a trip is expected to cost.
class FareEstimate {
  /// A decimal string, for example "17250".
  final String total;
  final String currencyCode;

  const FareEstimate({required this.total, required this.currencyCode});
}

/// How a finished trip's fare was paid.
class TripSettlement {
  final String currencyCode;
  final String fareAmount;

  /// What left the rider's wallet.
  final String walletAmount;

  /// What the rider hands the driver in cash.
  final String cashAmount;

  /// Change the driver could not return, credited to the wallet instead.
  final String changeAmount;

  const TripSettlement({
    required this.currencyCode,
    required this.fareAmount,
    required this.walletAmount,
    required this.cashAmount,
    required this.changeAmount,
  });

  factory TripSettlement.fromJson(Map<String, dynamic> json) {
    return TripSettlement(
      currencyCode: json['currencyCode'] as String? ?? '',
      fareAmount: json['fareAmount'] as String? ?? '0',
      walletAmount: json['walletAmount'] as String? ?? '0',
      cashAmount: json['cashAmount'] as String? ?? '0',
      changeAmount: json['changeAmount'] as String? ?? '0',
    );
  }
}
