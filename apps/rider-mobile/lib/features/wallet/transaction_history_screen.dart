import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/lookups.dart';
import '../../theme/app_theme.dart';
import 'wallet_tx_row.dart';

/// Everything that moved in the rider's wallet, newest first (the last 50).
class TransactionHistoryScreen extends ConsumerWidget {
  const TransactionHistoryScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final transactions = ref.watch(walletTransactionsProvider);
    final currency = ref.watch(walletProvider).valueOrNull?.currencyCode ?? '';

    return Scaffold(
      backgroundColor: colors.surface200,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('سجل الحركات', style: textTheme.titleMedium),
      ),
      body: RefreshIndicator(
        onRefresh: () async {
          ref.invalidate(walletProvider);
          ref.invalidate(walletTransactionsProvider);
          await ref.read(walletTransactionsProvider.future);
        },
        child: transactions.when(
          data: (list) => list.isEmpty
              ? ListView(
                  children: [
                    const SizedBox(height: 120),
                    Center(child: Text('ما في حركات بعد', style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted))),
                  ],
                )
              : ListView(
                  padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5),
                  children: [
                    for (final tx in list)
                      Container(
                        decoration: BoxDecoration(border: Border(top: BorderSide(color: colors.border))),
                        child: WalletTxRow(tx: tx, currencyCode: currency),
                      ),
                  ],
                ),
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (_, __) => ListView(
            children: [
              const SizedBox(height: 120),
              Center(child: Text('تعذر تحميل الحركات. اسحب للأسفل لإعادة المحاولة', style: textTheme.bodyLarge?.copyWith(color: colors.danger))),
            ],
          ),
        ),
      ),
    );
  }
}
