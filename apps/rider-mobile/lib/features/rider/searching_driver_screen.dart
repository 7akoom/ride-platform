import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../state/home_events.dart';
import '../../theme/app_theme.dart';
import 'cancel_trip_sheet.dart';
import 'tracking_screen.dart';

/// Waits for a driver: asks the backend about the trip every two seconds. When a driver
/// accepts, it moves on to the tracking screen; when the backend gives up (no driver
/// accepted in about two minutes) or the rider cancels, it goes back home.
class SearchingDriverScreen extends ConsumerStatefulWidget {
  final Trip trip;

  /// Where the trip goes, for showing under the animation ('' when not known).
  final String destinationLabel;

  const SearchingDriverScreen({
    super.key,
    required this.trip,
    required this.destinationLabel,
  });

  @override
  ConsumerState<SearchingDriverScreen> createState() => _SearchingDriverScreenState();
}

class _SearchingDriverScreenState extends ConsumerState<SearchingDriverScreen> with SingleTickerProviderStateMixin {
  late final AnimationController _pulse;
  Timer? _pollTimer;
  bool _polling = false;
  bool _leaving = false;
  bool _cancelling = false;

  @override
  void initState() {
    super.initState();
    _pulse = AnimationController(vsync: this, duration: const Duration(milliseconds: 2400))..repeat();
    _pollTimer = Timer.periodic(const Duration(seconds: 2), (_) => _poll());
  }

  @override
  void dispose() {
    _pulse.dispose();
    _pollTimer?.cancel();
    super.dispose();
  }

  Future<void> _poll() async {
    if (_polling || _leaving || !mounted) return;
    _polling = true;

    try {
      final trip = await ref.read(tripApiProvider).getTrip(widget.trip.id);
      if (!mounted || _leaving) return;

      switch (trip.status) {
        case TripStatus.accepted:
        case TripStatus.inProgress:
          _leaving = true;
          _pollTimer?.cancel();
          Navigator.of(context).pushReplacement(
            MaterialPageRoute(
              builder: (_) => TrackingScreen(trip: trip, destinationLabel: widget.destinationLabel),
            ),
          );
        case TripStatus.cancelled:
          _goHome('لم نجد سائقاً قريباً حالياً. حاول مرة أخرى بعد قليل');
        case TripStatus.completed:
          _goHome('');
        case TripStatus.requested:
        case TripStatus.unknown:
          break;
      }
    } on ApiException {
      // A dropped connection is not the end of the search: the next poll tries again.
    } finally {
      _polling = false;
    }
  }

  void _goHome(String message) {
    if (_leaving) return;
    _leaving = true;
    _pollTimer?.cancel();

    ref.read(homeNoticeProvider.notifier).state = HomeNotice(message);
    Navigator.of(context).popUntil((route) => route.isFirst);
  }

  Future<void> _cancel() async {
    if (_cancelling) return;

    final reason = await showCancelTripSheet(context);
    if (reason == null || !mounted) return;

    setState(() => _cancelling = true);

    try {
      await ref.read(tripApiProvider).cancelTrip(widget.trip.id, reason);
      if (!mounted) return;
      _goHome('تم إلغاء الطلب');
    } on ApiException {
      if (!mounted) return;
      setState(() => _cancelling = false);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('تعذر إلغاء الطلب. حاول مرة أخرى')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return PopScope(
      canPop: false,
      child: Scaffold(
        backgroundColor: colors.surface100,
        body: Stack(
          children: [
            Positioned.fill(
              child: AnimatedBuilder(
                animation: _pulse,
                builder: (context, child) => CustomPaint(painter: _SearchMapPainter(_pulse.value, colors)),
              ),
            ),
            Align(
              alignment: Alignment.bottomCenter,
              child: Container(
                width: double.infinity,
                padding: const EdgeInsets.fromLTRB(AppSpacing.space5, AppSpacing.space6, AppSpacing.space5, AppSpacing.space6),
                decoration: BoxDecoration(
                  color: colors.surface200,
                  borderRadius: const BorderRadius.only(topLeft: Radius.circular(AppRadius.md), topRight: Radius.circular(AppRadius.md)),
                  boxShadow: [BoxShadow(color: colors.ink.withOpacity(0.1), blurRadius: 16, offset: const Offset(0, -2))],
                ),
                child: SafeArea(
                  top: false,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text('جاري البحث عن سائق قريب منك', style: textTheme.titleLarge, textAlign: TextAlign.center),
                      const SizedBox(height: AppSpacing.space3),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space4, vertical: AppSpacing.space2),
                        decoration: BoxDecoration(color: colors.brand100, borderRadius: BorderRadius.circular(AppRadius.full)),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Container(width: 8, height: 8, decoration: BoxDecoration(color: colors.brand500, shape: BoxShape.circle)),
                            const SizedBox(width: AppSpacing.space2),
                            Text('نتواصل مع السائقين القريبين', style: textTheme.labelLarge?.copyWith(color: colors.brand600)),
                          ],
                        ),
                      ),
                      if (widget.destinationLabel.isNotEmpty) ...[
                        const SizedBox(height: AppSpacing.space4),
                        Container(
                          width: double.infinity,
                          padding: const EdgeInsets.symmetric(vertical: AppSpacing.space3, horizontal: AppSpacing.space3),
                          decoration: BoxDecoration(color: colors.surface100, borderRadius: BorderRadius.circular(AppRadius.sm)),
                          child: Row(
                            mainAxisAlignment: MainAxisAlignment.center,
                            children: [
                              Icon(Icons.place_outlined, size: 14, color: colors.inkMuted),
                              const SizedBox(width: AppSpacing.space2),
                              Flexible(
                                child: Text(
                                  widget.destinationLabel,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                                ),
                              ),
                            ],
                          ),
                        ),
                      ],
                      const SizedBox(height: AppSpacing.space4),
                      OutlinedButton(
                        onPressed: _cancelling ? null : _cancel,
                        style: OutlinedButton.styleFrom(foregroundColor: colors.danger, side: BorderSide(color: colors.danger, width: 1.5), minimumSize: const Size.fromHeight(48)),
                        child: const Text('إلغاء الطلب'),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SearchMapPainter extends CustomPainter {
  final double t;
  final AppColors colors;

  _SearchMapPainter(this.t, this.colors);

  @override
  void paint(Canvas canvas, Size size) {
    final roadPaint = Paint()..color = colors.border..strokeWidth = 4;
    for (double y = 100; y < size.height; y += 130) {
      canvas.drawLine(Offset(0, y), Offset(size.width, y), roadPaint);
    }
    for (double x = 70; x < size.width; x += 130) {
      canvas.drawLine(Offset(x, 0), Offset(x, size.height), roadPaint);
    }

    final center = Offset(size.width / 2, size.height * 0.42);

    for (final delay in [0.0, 0.33, 0.66]) {
      final localT = (t + delay) % 1.0;
      final radius = 26 + localT * 60;
      final opacity = (1 - localT).clamp(0.0, 1.0) * 0.6;
      canvas.drawCircle(center, radius, Paint()..color = colors.brand500.withOpacity(opacity)..style = PaintingStyle.stroke..strokeWidth = 2);
    }

    final carOffsets = [
      Offset(center.dx - 75, center.dy - 60),
      Offset(center.dx + 70, center.dy - 50),
      Offset(center.dx + 55, center.dy + 80),
    ];
    final delays = [0.15, 0.4, 0.7];
    for (var i = 0; i < carOffsets.length; i++) {
      final localT = ((t + delays[i]) % 1.0);
      final appear = (localT < 0.5) ? (localT * 2).clamp(0.0, 1.0) : 1.0;
      canvas.drawCircle(carOffsets[i], 12 * appear, Paint()..color = Colors.white.withOpacity(appear));
      canvas.drawCircle(carOffsets[i], 12 * appear, Paint()..color = colors.brand600.withOpacity(appear)..style = PaintingStyle.stroke..strokeWidth = 1.5);
    }

    canvas.drawCircle(center, 9, Paint()..color = colors.brand500);
    canvas.drawCircle(center, 9, Paint()..color = Colors.white..style = PaintingStyle.stroke..strokeWidth = 3);
  }

  @override
  bool shouldRepaint(covariant _SearchMapPainter oldDelegate) => true;
}
