import 'package:flutter/material.dart';

import '../../core/format.dart';
import '../../core/models/wallet_tx.dart';
import '../../theme/app_theme.dart';

/// One line of the wallet history: what it was, when, and how much.
class WalletTxRow extends StatelessWidget {
  final WalletTx tx;
  final String currencyCode;

  const WalletTxRow({super.key, required this.tx, required this.currencyCode});

  IconData get _icon {
    switch (tx.type) {
      case WalletTxType.topUp:
        return Icons.arrow_upward;
      case WalletTxType.tripPayment:
        return Icons.arrow_downward;
      case WalletTxType.changeCredit:
        return Icons.savings_outlined;
      case WalletTxType.adjustment:
      case WalletTxType.other:
        return tx.isCredit ? Icons.arrow_upward : Icons.arrow_downward;
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final credit = tx.isCredit;

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: AppSpacing.space3),
      child: Row(
        children: [
          Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(color: credit ? colors.brand100 : colors.surface100, shape: BoxShape.circle),
            child: Icon(_icon, size: 16, color: credit ? colors.brand600 : colors.inkMuted),
          ),
          const SizedBox(width: AppSpacing.space3),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(tx.label, style: textTheme.titleMedium?.copyWith(fontSize: 14)),
                Text(formatTripDate(tx.createdAt), style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
              ],
            ),
          ),
          Text(
            formatSignedMoney(tx.amount, currencyCode),
            style: textTheme.titleMedium?.copyWith(fontSize: 14, color: credit ? colors.success : colors.ink),
          ),
        ],
      ),
    );
  }
}
