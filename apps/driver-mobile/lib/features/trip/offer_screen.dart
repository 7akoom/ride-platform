import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/format.dart';
import '../../core/models/place.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../state/lookups.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';

/// A trip put to the driver, with a few seconds to take it or turn it down. Pops with the
/// [Trip] if the driver accepted, and with null otherwise (turned down, expired, or gone).
class OfferScreen extends ConsumerStatefulWidget {
  final TripOffer offer;

  const OfferScreen({super.key, required this.offer});

  @override
  ConsumerState<OfferScreen> createState() => _OfferScreenState();
}

class _OfferScreenState extends ConsumerState<OfferScreen> {
  Timer? _ticker;
  bool _busy = false;
  bool _done = false;

  RouteInfo? _toPickup;
  RouteInfo? _theTrip;

  Duration get _total {
    final offered = widget.offer.offeredAt;
    final expires = widget.offer.expiresAt;

    if (offered != null && expires != null) {
      final total = expires.difference(offered);
      if (total > Duration.zero) return total;
    }

    return const Duration(seconds: 15);
  }

  Duration get _left {
    final expires = widget.offer.expiresAt;
    if (expires == null) return const Duration(seconds: 15);

    final left = expires.difference(DateTime.now());

    return left.isNegative ? Duration.zero : left;
  }

  @override
  void initState() {
    super.initState();
    HapticFeedback.heavyImpact();

    _ticker = Timer.periodic(const Duration(milliseconds: 250), (_) {
      if (!mounted || _done) return;

      if (_left == Duration.zero) {
        _finish(null);
      } else {
        setState(() {});
      }
    });

    _loadRoutes();
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  Future<void> _loadRoutes() async {
    final maps = ref.read(mapsApiProvider);
    final position = ref.read(driverPositionProvider);

    try {
      if (position != null) {
        final toPickup = await maps.route(position, widget.offer.pickup);
        if (!mounted) return;
        setState(() => _toPickup = toPickup);
      }

      final theTrip = await maps.route(widget.offer.pickup, widget.offer.dropoff);
      if (!mounted) return;
      setState(() => _theTrip = theTrip);
    } on ApiException {
      // The numbers are a nicety: the offer can be answered without them.
    }
  }

  void _finish(Trip? trip) {
    if (_done) return;
    _done = true;
    _ticker?.cancel();

    if (mounted) Navigator.of(context).pop(trip);
  }

  Future<void> _accept() async {
    if (_busy || _done) return;
    setState(() => _busy = true);

    try {
      final driverId = await SessionStorage.readDriverId();
      if (driverId == null) {
        throw const ApiException(statusCode: 401, message: 'there is no driver profile');
      }

      final trip = await ref.read(tripApiProvider).acceptOffer(
            tripId: widget.offer.tripId,
            driverId: driverId,
          );

      _finish(trip);
    } on ApiException {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('العرض ما عاد متاح')),
      );
      _finish(null);
    }
  }

  Future<void> _reject() async {
    if (_busy || _done) return;
    setState(() => _busy = true);

    try {
      final driverId = await SessionStorage.readDriverId();
      if (driverId != null) {
        await ref.read(tripApiProvider).rejectOffer(tripId: widget.offer.tripId, driverId: driverId);
      }
    } on ApiException {
      // The offer runs out by itself anyway.
    }

    _finish(null);
  }

  String _km(double meters) => arabicDigits((meters / 1000).toStringAsFixed(1));

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final pickupLabel = ref.watch(placeLabelProvider(widget.offer.pickup)).valueOrNull ?? '...';
    final dropoffLabel = ref.watch(placeLabelProvider(widget.offer.dropoff)).valueOrNull ?? '...';
    final progress = math.max(0.0, math.min(1.0, _left.inMilliseconds / _total.inMilliseconds));
    final seconds = (_left.inMilliseconds / 1000).ceil();

    return PopScope(
      canPop: false,
      child: Scaffold(
        backgroundColor: colors.surface200,
        body: SafeArea(
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.space5),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const SizedBox(height: AppSpacing.space5),
                Text('طلب رحلة جديد', textAlign: TextAlign.center, style: textTheme.titleLarge),
                const SizedBox(height: AppSpacing.space4),
                ClipRRect(
                  borderRadius: BorderRadius.circular(AppRadius.full),
                  child: LinearProgressIndicator(
                    value: progress,
                    minHeight: 8,
                    backgroundColor: colors.surface100,
                    color: colors.brand500,
                  ),
                ),
                const SizedBox(height: AppSpacing.space2),
                Text('${arabicDigits('$seconds')} ثانية', textAlign: TextAlign.center, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                const SizedBox(height: AppSpacing.space5),
                Container(
                  padding: const EdgeInsets.all(AppSpacing.space4),
                  decoration: BoxDecoration(
                    border: Border.all(color: colors.border),
                    borderRadius: BorderRadius.circular(AppRadius.md),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Icon(Icons.circle, size: 10, color: colors.success),
                          const SizedBox(width: AppSpacing.space2),
                          Expanded(child: Text(pickupLabel, style: textTheme.titleMedium)),
                        ],
                      ),
                      if (_toPickup != null)
                        Padding(
                          padding: const EdgeInsets.only(right: AppSpacing.space4, top: 2),
                          child: Text(
                            'تبعد عنك ${_km(_toPickup!.distanceMeters)} كم · ${formatMinutes(_toPickup!.durationSeconds)}',
                            style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                          ),
                        ),
                      const SizedBox(height: AppSpacing.space3),
                      Row(
                        children: [
                          Icon(Icons.place_outlined, size: 18, color: colors.brand500),
                          const SizedBox(width: AppSpacing.space2),
                          Expanded(child: Text(dropoffLabel, style: textTheme.titleMedium)),
                        ],
                      ),
                      if (_theTrip != null)
                        Padding(
                          padding: const EdgeInsets.only(right: AppSpacing.space4, top: 2),
                          child: Text(
                            'طول الرحلة ${_km(_theTrip!.distanceMeters)} كم · ${formatMinutes(_theTrip!.durationSeconds)}',
                            style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                          ),
                        ),
                    ],
                  ),
                ),
                const SizedBox(height: AppSpacing.space3),
                Text(
                  widget.offer.paymentMethod == 'wallet' ? 'الدفع: من محفظة الراكب (وأي فرق نقداً)' : 'الدفع: نقداً',
                  style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                ),
                const Spacer(),
                ElevatedButton(
                  onPressed: _busy ? null : _accept,
                  child: _busy
                      ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                      : const Text('قبول الرحلة'),
                ),
                const SizedBox(height: AppSpacing.space2),
                TextButton(
                  onPressed: _busy ? null : _reject,
                  style: TextButton.styleFrom(foregroundColor: colors.danger),
                  child: const Text('رفض'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
