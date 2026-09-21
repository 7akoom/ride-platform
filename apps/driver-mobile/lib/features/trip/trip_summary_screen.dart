import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../core/format.dart';
import '../../core/models/settlement.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../theme/app_theme.dart';

/// The end of a trip: what to collect from the rider in cash, what the driver earned, the
/// change they could not give, and the rating of the rider.
class TripSummaryScreen extends ConsumerStatefulWidget {
  final Trip trip;

  const TripSummaryScreen({super.key, required this.trip});

  @override
  ConsumerState<TripSummaryScreen> createState() => _TripSummaryScreenState();
}

class _TripSummaryScreenState extends ConsumerState<TripSummaryScreen> {
  final _received = TextEditingController();

  DriverSettlement? _settlement;
  bool _loading = true;

  bool _recordingChange = false;
  bool _changeRecorded = false;
  String? _changeMessage;

  /// 0 means not chosen: no rating is sent then, instead of a made-up 5.
  int _rating = 0;
  bool _submitting = false;

  @override
  void initState() {
    super.initState();
    _loadSettlement();
  }

  @override
  void dispose() {
    _received.dispose();
    super.dispose();
  }

  /// The money is settled a few seconds after the trip ends: ask once a second (up to about
  /// 25 seconds) until it is there.
  Future<void> _loadSettlement() async {
    for (var attempt = 0; attempt < 25; attempt++) {
      try {
        final settlement = await ref.read(moneyApiProvider).tripSettlement(
              driverId: widget.trip.driverId,
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

    if (mounted) setState(() => _loading = false);
  }

  int get _cashDue => (double.tryParse(_settlement?.cashAmount ?? '') ?? 0).round();

  Future<void> _recordChange(int received) async {
    if (_recordingChange) return;

    setState(() {
      _recordingChange = true;
      _changeMessage = null;
    });

    try {
      final credited = await ref.read(moneyApiProvider).recordChange(
            driverId: widget.trip.driverId,
            tripId: widget.trip.id,
            cashReceived: received,
          );

      if (!mounted) return;
      setState(() {
        _recordingChange = false;
        _changeRecorded = true;
        _changeMessage = 'أُضيفت الفكّة (${formatMoney(credited, _settlement?.currencyCode ?? '')}) لمحفظة الراكب';
      });
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _recordingChange = false;
        _changeMessage = describeFailure(
          error,
          wrong: 'ما قدرنا نسجّل الفكّة: المبلغ فوق الحد المسموح، أو سبق وسجّلتها',
        );
      });
    }
  }

  Future<void> _done() async {
    if (_submitting) return;
    setState(() => _submitting = true);

    if (_rating > 0) {
      try {
        await ref.read(tripApiProvider).rateRider(tripId: widget.trip.id, stars: _rating);
      } on ApiException {
        // A rating that could not be sent must not trap the driver here.
      }
    }

    if (!mounted) return;
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
                padding: const EdgeInsets.fromLTRB(AppSpacing.space5, AppSpacing.space6, AppSpacing.space5, AppSpacing.space4),
                child: Column(
                  children: [
                    Container(
                      width: 64,
                      height: 64,
                      decoration: BoxDecoration(color: colors.success, shape: BoxShape.circle),
                      child: const Icon(Icons.check, color: Colors.white, size: 30),
                    ),
                    const SizedBox(height: AppSpacing.space3),
                    Text('انتهت الرحلة', style: textTheme.titleLarge),
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
                                    'سيظهر مبلغ الرحلة في أرباحك بعد قليل',
                                    style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14),
                                  )
                                : _Earnings(settlement: settlement, colors: colors, textTheme: textTheme),
                      ),
                      if (settlement != null && _cashDue > 0) ...[
                        const SizedBox(height: AppSpacing.space4),
                        _cashSection(settlement, colors, textTheme),
                      ],
                      const SizedBox(height: AppSpacing.space6),
                      Text('قيّم الراكب', textAlign: TextAlign.center, style: textTheme.titleMedium),
                      const SizedBox(height: AppSpacing.space2),
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
                  onPressed: _submitting ? null : _done,
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

  /// What to collect in cash, and what to do when the rider hands over more than the fare and
  /// the driver has no change.
  Widget _cashSection(DriverSettlement settlement, AppColors colors, TextTheme textTheme) {
    final currency = settlement.currencyCode;
    final received = parseAmount(_received.text);
    final change = received == null ? 0 : received - _cashDue;
    final alreadyCredited = (double.tryParse(settlement.changeAmount) ?? 0) > 0;

    return Container(
      padding: const EdgeInsets.all(AppSpacing.space4),
      decoration: BoxDecoration(color: colors.brand100, borderRadius: BorderRadius.circular(AppRadius.md)),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('حصّل من الراكب نقداً', style: textTheme.bodyLarge?.copyWith(color: colors.brand600, fontSize: 14)),
          const SizedBox(height: AppSpacing.space1),
          Text(
            formatMoney(settlement.cashAmount, currency),
            style: textTheme.displayMedium?.copyWith(color: colors.brand600, fontSize: 28),
          ),
          const SizedBox(height: AppSpacing.space4),
          if (alreadyCredited)
            Text(
              'أُضيفت فكّة ${formatMoney(settlement.changeAmount, currency)} لمحفظة الراكب',
              style: textTheme.bodySmall?.copyWith(color: colors.brand600),
            )
          else ...[
            Text('المبلغ اللي سلّمك إياه الراكب', style: textTheme.labelLarge),
            const SizedBox(height: AppSpacing.space2),
            TextField(
              controller: _received,
              keyboardType: TextInputType.number,
              enabled: !_changeRecorded,
              onChanged: (_) => setState(() {}),
              decoration: InputDecoration(
                hintText: 'مثال: ${arabicDigits('${_cashDue + 1000}')}',
                filled: true,
                fillColor: colors.surface200,
                border: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide.none),
              ),
            ),
            if (received != null && change > 0 && !_changeRecorded) ...[
              const SizedBox(height: AppSpacing.space3),
              Text(
                'الفكّة اللي عليك: ${formatMoney('$change', currency)}',
                style: textTheme.titleMedium?.copyWith(color: colors.brand600),
              ),
              const SizedBox(height: AppSpacing.space1),
              Text(
                'إذا ما عندك فكّة، تُضاف للمحفظة الراكب وما تنخصم منك.',
                style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
              ),
              const SizedBox(height: AppSpacing.space2),
              OutlinedButton(
                onPressed: _recordingChange ? null : () => _recordChange(received),
                child: _recordingChange
                    ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
                    : const Text('ما عندي فكّة، أضفها لمحفظة الراكب'),
              ),
            ],
            if (received != null && change < 0) ...[
              const SizedBox(height: AppSpacing.space2),
              Text(
                'المبلغ أقل من المطلوب بـ ${formatMoney('${-change}', currency)}',
                style: textTheme.bodySmall?.copyWith(color: colors.danger),
              ),
            ],
          ],
          if (_changeMessage != null) ...[
            const SizedBox(height: AppSpacing.space2),
            Text(
              _changeMessage!,
              style: textTheme.bodySmall?.copyWith(color: _changeRecorded ? colors.brand600 : colors.danger),
            ),
          ],
        ],
      ),
    );
  }
}

class _Earnings extends StatelessWidget {
  final DriverSettlement settlement;
  final AppColors colors;
  final TextTheme textTheme;

  const _Earnings({
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
            Text('ربحك من الرحلة', style: textTheme.titleMedium),
            Text(
              formatMoney(settlement.driverEarning, currency),
              style: textTheme.displayMedium?.copyWith(fontSize: 22),
            ),
          ],
        ),
        Padding(
          padding: const EdgeInsets.symmetric(vertical: AppSpacing.space2),
          child: Divider(height: 1, color: colors.border),
        ),
        _Line('أجرة الرحلة', formatMoney(settlement.fareAmount, currency), textTheme, colors),
        _Line('عمولة المنصة', formatMoney(settlement.commissionAmount, currency), textTheme, colors),
        if (_positive(settlement.walletAmount))
          _Line('دُفع من محفظة الراكب', formatMoney(settlement.walletAmount, currency), textTheme, colors),
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
