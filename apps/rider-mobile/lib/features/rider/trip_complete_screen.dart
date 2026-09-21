import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/format.dart';
import '../../core/models/fare.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../state/home_events.dart';
import '../../theme/app_theme.dart';

/// The end of a trip: what it cost and how it was paid, and the rating of the driver.
///
/// The way to pay (cash or wallet) is not chosen here any more: the backend fixes it when
/// the trip is requested, so the choice moved to the request screen.
class TripCompleteScreen extends ConsumerStatefulWidget {
  final Trip trip;
  final String destinationLabel;

  const TripCompleteScreen({
    super.key,
    required this.trip,
    required this.destinationLabel,
  });

  @override
  ConsumerState<TripCompleteScreen> createState() => _TripCompleteScreenState();
}

class _TripCompleteScreenState extends ConsumerState<TripCompleteScreen> {
  TripSettlement? _settlement;
  bool _loading = true;
  bool _submitting = false;

  /// 0 means the rider has not chosen: no rating is sent then, instead of a made-up 5.
  int _rating = 0;

  @override
  void initState() {
    super.initState();
    _loadSettlement();
  }

  /// The money is settled a few seconds after the trip ends, so this asks once a second
  /// until it is there (up to about 25 seconds).
  Future<void> _loadSettlement() async {
    for (var attempt = 0; attempt < 25; attempt++) {
      try {
        final settlement = await ref.read(moneyApiProvider).tripSettlement(
              riderId: widget.trip.riderId,
              tripId: widget.trip.id,
            );

        if (!mounted) return;

        if (settlement != null) {
          setState(() {
            _settlement = settlement;
            _loading = false;
          });
          return;
        }
      } on ApiException {
        // Try again: the settlement may just not be ready.
      }

      await Future<void>.delayed(const Duration(seconds: 1));
      if (!mounted) return;
    }

    if (mounted) {
      setState(() => _loading = false);
    }
  }

  Future<void> _confirmDone() async {
    if (_submitting) return;
    setState(() => _submitting = true);

    if (_rating > 0) {
      try {
        await ref.read(tripApiProvider).rateDriver(tripId: widget.trip.id, stars: _rating);
      } on ApiException {
        // A rating that could not be sent (or was already sent) must not trap the rider here.
      }
    }

    if (!mounted) return;

    ref.read(homeNoticeProvider.notifier).state = const HomeNotice('');
    Navigator.of(context).popUntil((route) => route.isFirst);
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final settlement = _settlement;

    return PopScope(
      canPop: false,
      child: Scaffold(
        backgroundColor: colors.surface200,
        body: SafeArea(
          child: Column(
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(AppSpacing.space5, AppSpacing.space6, AppSpacing.space5, AppSpacing.space5),
                child: Column(
                  children: [
                    Container(
                      width: 64,
                      height: 64,
                      decoration: BoxDecoration(color: colors.success, shape: BoxShape.circle),
                      child: const Icon(Icons.check, color: Colors.white, size: 30),
                    ),
                    const SizedBox(height: AppSpacing.space3),
                    Text('تمت الرحلة بنجاح', style: textTheme.titleLarge),
                    const SizedBox(height: AppSpacing.space1),
                    Text(
                      widget.destinationLabel.isEmpty ? 'شكراً لاستخدامك منصتنا' : widget.destinationLabel,
                      textAlign: TextAlign.center,
                      style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14),
                    ),
                  ],
                ),
              ),
              Expanded(
                child: SingleChildScrollView(
                  padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Container(
                        padding: const EdgeInsets.all(AppSpacing.space4 + 2),
                        decoration: BoxDecoration(
                          border: Border.all(color: colors.border),
                          borderRadius: BorderRadius.circular(AppRadius.md),
                        ),
                        child: _loading
                            ? Row(
                                mainAxisAlignment: MainAxisAlignment.center,
                                children: [
                                  const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2)),
                                  const SizedBox(width: AppSpacing.space3),
                                  Text('جاري حساب الأجرة...', style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14)),
                                ],
                              )
                            : settlement == null
                                ? Text(
                                    'سيظهر مبلغ الرحلة في سجل رحلاتك بعد قليل',
                                    style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14),
                                  )
                                : _SettlementLines(settlement: settlement, colors: colors, textTheme: textTheme),
                      ),
                      const SizedBox(height: AppSpacing.space6),
                      Text('قيّم رحلتك', textAlign: TextAlign.center, style: textTheme.titleMedium),
                      const SizedBox(height: AppSpacing.space3),
                      Row(
                        mainAxisAlignment: MainAxisAlignment.center,
                        children: List.generate(5, (i) {
                          final n = i + 1;
                          return IconButton(
                            onPressed: () => setState(() => _rating = n),
                            icon: Icon(n <= _rating ? Icons.star : Icons.star_border, color: colors.warning, size: 32),
                          );
                        }),
                      ),
                      const SizedBox(height: AppSpacing.space4),
                    ],
                  ),
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(AppSpacing.space5, AppSpacing.space3, AppSpacing.space5, AppSpacing.space5),
                child: ElevatedButton(
                  onPressed: _submitting ? null : _confirmDone,
                  child: _submitting
                      ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                      : const Text('تم'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The fare, and how it was paid: what left the wallet, what is handed to the driver in
/// cash, and any change that went to the wallet instead.
class _SettlementLines extends StatelessWidget {
  final TripSettlement settlement;
  final AppColors colors;
  final TextTheme textTheme;

  const _SettlementLines({
    required this.settlement,
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
            Text(
              formatMoney(settlement.fareAmount, currency),
              style: textTheme.displayMedium?.copyWith(fontSize: 22),
            ),
          ],
        ),
        if (_positive(settlement.walletAmount)) ...[
          const SizedBox(height: AppSpacing.space3),
          _Line('خُصم من محفظتك', formatMoney(settlement.walletAmount, currency), textTheme, colors),
        ],
        if (_positive(settlement.cashAmount)) ...[
          const SizedBox(height: AppSpacing.space3),
          Container(
            padding: const EdgeInsets.all(AppSpacing.space3),
            decoration: BoxDecoration(color: colors.brand100, borderRadius: BorderRadius.circular(AppRadius.sm)),
            child: Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Text('ادفع للسائق نقداً', style: textTheme.titleMedium?.copyWith(color: colors.brand600)),
                Text(
                  formatMoney(settlement.cashAmount, currency),
                  style: textTheme.titleMedium?.copyWith(color: colors.brand600),
                ),
              ],
            ),
          ),
        ],
        if (_positive(settlement.changeAmount)) ...[
          const SizedBox(height: AppSpacing.space3),
          _Line('فكّة أُضيفت لمحفظتك', formatMoney(settlement.changeAmount, currency), textTheme, colors),
        ],
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
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text(label, style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14)),
        Text(amount, style: textTheme.bodyLarge?.copyWith(fontSize: 14)),
      ],
    );
  }
}
