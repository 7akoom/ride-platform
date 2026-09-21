import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/format.dart';
import '../../state/lookups.dart';
import '../../theme/app_theme.dart';
import 'transaction_history_screen.dart';
import 'wallet_tx_row.dart';

/// The rider's wallet: the balance and the latest movements, from the backend.
///
/// Topping up and sending money are not there yet (the backend can charge a driver's wallet
/// through ZainCash but has no way for a rider to add money, and no rider-to-rider
/// transfers), so those buttons say "coming soon" instead of doing nothing.
class WalletHomeScreen extends ConsumerWidget {
  const WalletHomeScreen({super.key});

  void _soon(BuildContext context) {
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('هذه الميزة قريباً')),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final wallet = ref.watch(walletProvider);
    final transactions = ref.watch(walletTransactionsProvider);
    final currency = wallet.valueOrNull?.currencyCode ?? '';

    return Scaffold(
      backgroundColor: colors.surface100,
      body: SafeArea(
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space4, vertical: AppSpacing.space3),
              child: Row(
                children: [
                  IconButton(
                    padding: EdgeInsets.zero,
                    constraints: const BoxConstraints(),
                    icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                    onPressed: () => Navigator.of(context).maybePop(),
                  ),
                  const SizedBox(width: AppSpacing.space2),
                  Text('المحفظة', style: textTheme.titleMedium),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5),
              child: Container(
                width: double.infinity,
                padding: const EdgeInsets.all(AppSpacing.space5),
                decoration: BoxDecoration(color: colors.brand500, borderRadius: BorderRadius.circular(AppRadius.md)),
                child: Stack(
                  children: [
                    Positioned(
                      left: -8,
                      bottom: -6,
                      child: Opacity(
                        opacity: 0.5,
                        child: CustomPaint(size: const Size(90, 60), painter: _RouteDotsPainter()),
                      ),
                    ),
                    Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('الرصيد المتاح', style: textTheme.bodySmall?.copyWith(color: Colors.white.withOpacity(0.8))),
                        const SizedBox(height: AppSpacing.space2),
                        Text(
                          wallet.when(
                            data: (w) => formatMoney(w.balance, w.currencyCode),
                            loading: () => '...',
                            error: (_, __) => 'تعذر التحميل',
                          ),
                          style: textTheme.displayMedium?.copyWith(color: Colors.white),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(vertical: AppSpacing.space5),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceAround,
                children: [
                  _QuickAction(icon: Icons.add, label: 'تعبئة', onTap: () => _soon(context), colors: colors, textTheme: textTheme),
                  _QuickAction(
                    icon: Icons.arrow_forward,
                    label: 'إرسال',
                    onTap: () => _soon(context),
                    colors: colors,
                    textTheme: textTheme,
                  ),
                  _QuickAction(
                    icon: Icons.receipt_long_outlined,
                    label: 'السجل',
                    onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const TransactionHistoryScreen())),
                    colors: colors,
                    textTheme: textTheme,
                  ),
                ],
              ),
            ),
            Expanded(
              child: Container(
                width: double.infinity,
                decoration: BoxDecoration(
                  color: colors.surface200,
                  borderRadius: const BorderRadius.only(topLeft: Radius.circular(20), topRight: Radius.circular(20)),
                ),
                padding: const EdgeInsets.all(AppSpacing.space5),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      mainAxisAlignment: MainAxisAlignment.spaceBetween,
                      children: [
                        Text('آخر الحركات', style: textTheme.titleMedium),
                        GestureDetector(
                          onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const TransactionHistoryScreen())),
                          child: Text('عرض الكل', style: textTheme.bodySmall?.copyWith(color: colors.brand500, fontWeight: FontWeight.w600)),
                        ),
                      ],
                    ),
                    transactions.when(
                      data: (list) => list.isEmpty
                          ? Padding(
                              padding: const EdgeInsets.symmetric(vertical: AppSpacing.space4),
                              child: Text('ما في حركات بعد', style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                            )
                          : Column(
                              children: [
                                for (final tx in list.take(3)) WalletTxRow(tx: tx, currencyCode: currency),
                              ],
                            ),
                      loading: () => const Padding(
                        padding: EdgeInsets.symmetric(vertical: AppSpacing.space4),
                        child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
                      ),
                      error: (_, __) => Padding(
                        padding: const EdgeInsets.symmetric(vertical: AppSpacing.space4),
                        child: Text('تعذر تحميل الحركات', style: textTheme.bodySmall?.copyWith(color: colors.danger)),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _QuickAction extends StatelessWidget {
  final IconData icon;
  final String label;
  final VoidCallback onTap;
  final AppColors colors;
  final TextTheme textTheme;

  const _QuickAction({required this.icon, required this.label, required this.onTap, required this.colors, required this.textTheme});

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.full),
      child: Column(
        children: [
          Container(
            width: 52,
            height: 52,
            decoration: BoxDecoration(color: colors.surface200, shape: BoxShape.circle, boxShadow: const [BoxShadow(color: Color(0x14000000), blurRadius: 4)]),
            child: Icon(icon, color: colors.brand500),
          ),
          const SizedBox(height: AppSpacing.space2),
          Text(label, style: textTheme.labelLarge),
        ],
      ),
    );
  }
}

class _RouteDotsPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final linePaint = Paint()..color = Colors.white.withOpacity(0.3)..strokeWidth = 1.5..style = PaintingStyle.stroke;
    final path = Path()..moveTo(5, 45)..quadraticBezierTo(35, 10, 85, 20);
    canvas.drawPath(path, linePaint);
    final dotPaint = Paint()..color = Colors.white.withOpacity(0.6);
    canvas.drawCircle(const Offset(5, 45), 4, dotPaint);
    canvas.drawCircle(const Offset(45, 18), 3.5, dotPaint);
    canvas.drawCircle(const Offset(85, 20), 5, dotPaint);
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
