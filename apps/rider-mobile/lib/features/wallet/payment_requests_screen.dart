import 'package:flutter/material.dart';

import '../../theme/app_theme.dart';
import 'request_money_screen.dart';

enum RequestStatus { pending, paid, declined }

class PaymentRequest {
  final String name;
  final String initial;
  final String amount;
  final String subtitle;
  final RequestStatus status;
  final bool incoming;

  const PaymentRequest({
    required this.name,
    required this.initial,
    required this.amount,
    required this.subtitle,
    required this.status,
    required this.incoming,
  });
}

// TODO(robert): replace with Wallet.ListPaymentRequests through the
// Gateway once the RPC exists (see the backend roadmap).
const _demoRequests = [
  PaymentRequest(name: 'محمد علي', initial: 'م', amount: '٢٬٥٠٠ د.ع', subtitle: '"نصف أجرة التاكسي" · منذ ٥ دقائق', status: RequestStatus.pending, incoming: true),
  PaymentRequest(name: 'سارة أحمد', initial: 'س', amount: '٣٬٠٠٠ د.ع', subtitle: 'أمس', status: RequestStatus.paid, incoming: false),
  PaymentRequest(name: 'حسن كاظم', initial: 'ح', amount: '١٬٥٠٠ د.ع', subtitle: '١٤ سبتمبر', status: RequestStatus.declined, incoming: false),
];

class PaymentRequestsScreen extends StatelessWidget {
  const PaymentRequestsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final incoming = _demoRequests.where((r) => r.incoming && r.status == RequestStatus.pending).toList();
    final past = _demoRequests.where((r) => !(r.incoming && r.status == RequestStatus.pending)).toList();

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('طلبات الدفع', style: textTheme.titleMedium),
        actions: [
          IconButton(
            icon: const Icon(Icons.add),
            onPressed: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const RequestMoneyScreen())),
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.space5),
        children: [
          if (incoming.isNotEmpty) ...[
            Text('طلب جديد لك', style: textTheme.labelLarge?.copyWith(color: colors.inkMuted)),
            const SizedBox(height: AppSpacing.space2),
            for (final r in incoming)
              Container(
                margin: const EdgeInsets.only(bottom: AppSpacing.space4),
                padding: const EdgeInsets.all(AppSpacing.space4),
                decoration: BoxDecoration(
                  color: colors.surface200,
                  border: Border.all(color: colors.warning, width: 1.5),
                  borderRadius: BorderRadius.circular(AppRadius.md),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      children: [
                        CircleAvatar(radius: 20, backgroundColor: colors.brand100, child: Text(r.initial, style: TextStyle(color: colors.brand600, fontWeight: FontWeight.w600))),
                        const SizedBox(width: AppSpacing.space3),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text('${r.name} طلب منك ${r.amount}', style: textTheme.titleMedium?.copyWith(fontSize: 14)),
                              Text(r.subtitle, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                            ],
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: AppSpacing.space3),
                    Row(
                      children: [
                        Expanded(
                          child: OutlinedButton(
                            onPressed: () {},
                            style: OutlinedButton.styleFrom(foregroundColor: colors.inkMuted, side: BorderSide(color: colors.border), minimumSize: const Size.fromHeight(40)),
                            child: const Text('رفض'),
                          ),
                        ),
                        const SizedBox(width: AppSpacing.space2 + 2),
                        Expanded(
                          child: ElevatedButton(
                            onPressed: () {},
                            style: ElevatedButton.styleFrom(minimumSize: const Size.fromHeight(40)),
                            child: const Text('دفع'),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
          ],
          Text('طلباتك السابقة', style: textTheme.labelLarge?.copyWith(color: colors.inkMuted)),
          const SizedBox(height: AppSpacing.space2),
          for (final r in past)
            Container(
              padding: const EdgeInsets.symmetric(vertical: AppSpacing.space3),
              decoration: BoxDecoration(border: Border(top: BorderSide(color: colors.border))),
              child: Row(
                children: [
                  CircleAvatar(radius: 18, backgroundColor: colors.surface100, child: Text(r.initial, style: TextStyle(color: colors.inkMuted, fontWeight: FontWeight.w600, fontSize: 13))),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('طلبت من ${r.name}', style: textTheme.titleMedium?.copyWith(fontSize: 14)),
                        Text('${r.subtitle} · ${r.amount}', style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                      ],
                    ),
                  ),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space2 + 2, vertical: AppSpacing.space1),
                    decoration: BoxDecoration(
                      color: r.status == RequestStatus.paid ? colors.brand100 : colors.surface100,
                      borderRadius: BorderRadius.circular(AppRadius.full),
                    ),
                    child: Text(
                      r.status == RequestStatus.paid ? 'تم الدفع' : 'مرفوض',
                      style: textTheme.bodySmall?.copyWith(
                        color: r.status == RequestStatus.paid ? colors.success : colors.danger,
                        fontWeight: FontWeight.w600,
                      ),
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
