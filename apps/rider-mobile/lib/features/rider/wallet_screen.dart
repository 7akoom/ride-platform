import 'package:flutter/material.dart';

import '../../theme/app_theme.dart';

class WalletTransaction {
  final String label;
  final String date;
  final String amount;
  final bool isCredit;

  const WalletTransaction({
    required this.label,
    required this.date,
    required this.amount,
    required this.isCredit,
  });
}

// TODO(robert): replace with Wallet.GetWallet + Wallet.ListTransactions
// through the Gateway, for the signed-in rider's OWNER_TYPE_RIDER wallet.
const _demoTransactions = [
  WalletTransaction(
    label: 'رحلة · مركز أربيل التجاري',
    date: 'أمس · ٧:٤٥ م',
    amount: '−٤٬٢٠٠ د.ع',
    isCredit: false,
  ),
  WalletTransaction(
    label: 'شحن المحفظة',
    date: '١٤ سبتمبر · ١٠:٠٠ ص',
    amount: '+٢٠٬٠٠٠ د.ع',
    isCredit: true,
  ),
  WalletTransaction(
    label: 'خصم كوبون · WELCOME10',
    date: '١٤ سبتمبر · ١٠:٠٠ ص',
    amount: '+١٬٠٠٠ د.ع',
    isCredit: true,
  ),
];

class WalletScreen extends StatelessWidget {
  const WalletScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('المحفظة', style: textTheme.titleMedium),
      ),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.space4),
        children: [
          Container(
            padding: const EdgeInsets.all(AppSpacing.space5),
            decoration: BoxDecoration(
              color: colors.brand500,
              borderRadius: BorderRadius.circular(AppRadius.md),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'الرصيد الحالي',
                  style: textTheme.bodySmall?.copyWith(
                    color: Colors.white.withOpacity(0.85),
                  ),
                ),
                const SizedBox(height: AppSpacing.space2),
                Text(
                  '١٦٬٨٠٠ د.ع',
                  style: textTheme.displayMedium?.copyWith(
                    color: Colors.white,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: AppSpacing.space4),
          ElevatedButton(onPressed: () {}, child: const Text('شحن المحفظة')),
          const SizedBox(height: AppSpacing.space5),
          Text('آخر الحركات', style: textTheme.titleMedium),
          const SizedBox(height: AppSpacing.space2),
          for (final tx in _demoTransactions)
            Container(
              padding: const EdgeInsets.symmetric(
                vertical: AppSpacing.space3,
              ),
              decoration: BoxDecoration(
                border: Border(top: BorderSide(color: colors.border)),
              ),
              child: Row(
                children: [
                  Container(
                    width: 36,
                    height: 36,
                    decoration: BoxDecoration(
                      color: tx.isCredit ? colors.brand100 : colors.surface200,
                      shape: BoxShape.circle,
                    ),
                    child: Icon(
                      tx.isCredit ? Icons.arrow_upward : Icons.arrow_downward,
                      size: 16,
                      color: tx.isCredit ? colors.brand600 : colors.inkMuted,
                    ),
                  ),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(tx.label, style: textTheme.titleMedium),
                        Text(
                          tx.date,
                          style: textTheme.bodySmall?.copyWith(
                            color: colors.inkMuted,
                          ),
                        ),
                      ],
                    ),
                  ),
                  Text(
                    tx.amount,
                    style: textTheme.titleMedium?.copyWith(
                      color: tx.isCredit ? colors.success : colors.ink,
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}
