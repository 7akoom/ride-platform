/// How a finished trip was settled, as the driver sees it.
class DriverSettlement {
  final String currencyCode;
  final String fareAmount;

  /// What was taken from the rider's wallet.
  final String walletAmount;

  /// What the driver collects from the rider in cash.
  final String cashAmount;

  /// The platform's share.
  final String commissionAmount;

  /// What the driver earns from the trip.
  final String driverEarning;

  /// Change the platform credited to the rider's wallet because the driver had none.
  final String changeAmount;

  const DriverSettlement({
    required this.currencyCode,
    required this.fareAmount,
    required this.walletAmount,
    required this.cashAmount,
    required this.commissionAmount,
    required this.driverEarning,
    required this.changeAmount,
  });

  factory DriverSettlement.fromJson(Map<String, dynamic> json) {
    return DriverSettlement(
      currencyCode: json['currencyCode'] as String? ?? '',
      fareAmount: json['fareAmount'] as String? ?? '0',
      walletAmount: json['walletAmount'] as String? ?? '0',
      cashAmount: json['cashAmount'] as String? ?? '0',
      commissionAmount: json['commissionAmount'] as String? ?? '0',
      driverEarning: json['driverEarning'] as String? ?? '0',
      changeAmount: json['changeAmount'] as String? ?? '0',
    );
  }
}
