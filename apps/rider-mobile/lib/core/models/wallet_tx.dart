/// What a wallet movement was for.
enum WalletTxType { topUp, tripPayment, changeCredit, adjustment, other }

WalletTxType walletTxTypeFromJson(Object? value) {
  switch (value) {
    case 'TRANSACTION_TYPE_TOP_UP':
      return WalletTxType.topUp;
    case 'TRANSACTION_TYPE_TRIP_PAYMENT':
      return WalletTxType.tripPayment;
    case 'TRANSACTION_TYPE_CHANGE_CREDIT':
      return WalletTxType.changeCredit;
    case 'TRANSACTION_TYPE_ADJUSTMENT':
      return WalletTxType.adjustment;
    default:
      return WalletTxType.other;
  }
}

/// One line of the rider's wallet history.
class WalletTx {
  final String id;
  final WalletTxType type;

  /// A signed decimal string: negative means money left the wallet.
  final String amount;
  final String balanceAfter;
  final String tripId;
  final DateTime? createdAt;

  const WalletTx({
    required this.id,
    required this.type,
    required this.amount,
    required this.balanceAfter,
    required this.tripId,
    required this.createdAt,
  });

  factory WalletTx.fromJson(Map<String, dynamic> json) {
    final created = json['createdAt'];

    return WalletTx(
      id: json['id'] as String? ?? '',
      type: walletTxTypeFromJson(json['type']),
      amount: json['amount'] as String? ?? '0',
      balanceAfter: json['balanceAfter'] as String? ?? '0',
      tripId: json['tripId'] as String? ?? '',
      createdAt: created is String ? DateTime.tryParse(created) : null,
    );
  }

  bool get isCredit => (double.tryParse(amount) ?? 0) > 0;

  /// What to call it in the list.
  String get label {
    switch (type) {
      case WalletTxType.topUp:
        return 'شحن المحفظة';
      case WalletTxType.tripPayment:
        return 'دفع رحلة';
      case WalletTxType.changeCredit:
        return 'فكّة من السائق';
      case WalletTxType.adjustment:
        return 'تسوية';
      case WalletTxType.other:
        return 'حركة على المحفظة';
    }
  }
}
