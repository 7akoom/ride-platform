import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/format.dart';
import '../../core/models/fare.dart';
import '../../core/models/trip.dart';
import '../../state/lookups.dart';
import '../../theme/app_theme.dart';
import '../support/support_screen.dart';
import 'ride_history_screen.dart';

/// One trip: where it went, when, who drove, and how it was paid.
///
/// The fare is shown as the backend settled it (total, what left the wallet, what was paid
/// in cash, any change). A line-by-line breakdown (base fare, distance) is not kept after
/// the trip, so it is not shown.
class TripDetailScreen extends ConsumerWidget {
  final Trip trip;

  const TripDetailScreen({super.key, required this.trip});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final cancelled = trip.status == TripStatus.cancelled;

    final destination = ref.watch(placeLabelProvider(trip.dropoff)).valueOrNull ?? '...';
    final origin = ref.watch(placeLabelProvider(trip.pickup)).valueOrNull ?? '...';
    final settlement = trip.status == TripStatus.completed
        ? ref.watch(settlementProvider(trip.id)).valueOrNull
        : null;
    final driver = trip.hasDriver ? ref.watch(tripDriverProvider(trip.id)).valueOrNull : null;

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('تفاصيل الرحلة', style: textTheme.titleMedium),
      ),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.space4),
        children: [
          Container(
            padding: const EdgeInsets.all(AppSpacing.space4),
            decoration: BoxDecoration(
              color: colors.surface200,
              borderRadius: BorderRadius.circular(AppRadius.md),
              border: Border.all(color: colors.border),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(Icons.circle, size: 10, color: colors.success),
                    const SizedBox(width: AppSpacing.space2),
                    Expanded(child: Text(origin, style: textTheme.bodyLarge)),
                  ],
                ),
                const SizedBox(height: AppSpacing.space2),
                Row(
                  children: [
                    Icon(Icons.place_outlined, size: 18, color: colors.brand500),
                    const SizedBox(width: AppSpacing.space2),
                    Expanded(child: Text(destination, style: textTheme.titleMedium)),
                  ],
                ),
                const SizedBox(height: AppSpacing.space3),
                Text(
                  formatTripDate(trip.when),
                  style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                ),
                const SizedBox(height: AppSpacing.space1),
                Text(
                  tripStatusText(trip.status),
                  style: textTheme.bodySmall?.copyWith(color: cancelled ? colors.danger : colors.success),
                ),
                if (cancelled && trip.cancellationReason.isNotEmpty) ...[
                  const SizedBox(height: AppSpacing.space1),
                  Text(trip.cancellationReason, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                ],
              ],
            ),
          ),
          if (driver != null && (driver.name.isNotEmpty || driver.carInfo.isNotEmpty)) ...[
            const SizedBox(height: AppSpacing.space4),
            Container(
              padding: const EdgeInsets.all(AppSpacing.space4),
              decoration: BoxDecoration(
                border: Border.all(color: colors.border),
                borderRadius: BorderRadius.circular(AppRadius.md),
              ),
              child: Row(
                children: [
                  CircleAvatar(
                    radius: 22,
                    backgroundColor: colors.brand100,
                    child: driver.initial.isEmpty
                        ? Icon(Icons.person, color: colors.brand600)
                        : Text(driver.initial, style: TextStyle(color: colors.brand600, fontWeight: FontWeight.w600)),
                  ),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(driver.name.isEmpty ? 'السائق' : driver.name, style: textTheme.titleMedium),
                        if (driver.carInfo.isNotEmpty)
                          Text(driver.carInfo, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ],
          const SizedBox(height: AppSpacing.space4),
          if (trip.status == TripStatus.completed)
            Container(
              padding: const EdgeInsets.all(AppSpacing.space4),
              decoration: BoxDecoration(
                border: Border.all(color: colors.border),
                borderRadius: BorderRadius.circular(AppRadius.md),
              ),
              child: settlement == null
                  ? Text('جاري تحميل تفاصيل الدفع...', style: textTheme.bodySmall?.copyWith(color: colors.inkMuted))
                  : _PaymentLines(settlement: settlement, paymentMethod: trip.paymentMethod, colors: colors, textTheme: textTheme),
            ),
          const SizedBox(height: AppSpacing.space5),
          OutlinedButton.icon(
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute(
                builder: (_) => SupportScreen(tripContext: destination),
              ),
            ),
            icon: const Icon(Icons.flag_outlined, size: 18),
            label: const Text('واجهت مشكلة بهذي الرحلة؟'),
          ),
        ],
      ),
    );
  }
}

class _PaymentLines extends StatelessWidget {
  final TripSettlement settlement;
  final String paymentMethod;
  final AppColors colors;
  final TextTheme textTheme;

  const _PaymentLines({
    required this.settlement,
    required this.paymentMethod,
    required this.colors,
    required this.textTheme,
  });

  bool _positive(String amount) => (double.tryParse(amount) ?? 0) > 0;

  @override
  Widget build(BuildContext context) {
    final currency = settlement.currencyCode;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Text('الإجمالي', style: textTheme.titleMedium),
            Text(formatMoney(settlement.fareAmount, currency), style: textTheme.titleLarge),
          ],
        ),
        Padding(
          padding: const EdgeInsets.symmetric(vertical: AppSpacing.space2),
          child: Divider(height: 1, color: colors.border),
        ),
        _Line('طريقة الدفع', paymentMethod == 'wallet' ? 'المحفظة' : 'نقداً', textTheme, colors),
        if (_positive(settlement.walletAmount))
          _Line('خُصم من المحفظة', formatMoney(settlement.walletAmount, currency), textTheme, colors),
        if (_positive(settlement.cashAmount))
          _Line('دُفع نقداً للسائق', formatMoney(settlement.cashAmount, currency), textTheme, colors),
        if (_positive(settlement.changeAmount))
          _Line('فكّة أُضيفت للمحفظة', formatMoney(settlement.changeAmount, currency), textTheme, colors),
      ],
    );
  }
}

class _Line extends StatelessWidget {
  final String label;
  final String amount;
  final TextTheme textTheme;
  final AppColors colors;

  const _Line(this.label, this.amount, this.textTheme, this.colors);

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: AppSpacing.space1 + 1),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(label, style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14)),
          Text(amount, style: textTheme.bodyLarge?.copyWith(fontSize: 14)),
        ],
      ),
    );
  }
}
