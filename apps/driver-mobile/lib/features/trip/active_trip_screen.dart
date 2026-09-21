import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maplibre_gl/maplibre_gl.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../core/app_config.dart';
import '../../core/format.dart';
import '../../core/map_config.dart';
import '../../core/models/geo_point.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../state/lookups.dart';
import '../../theme/app_theme.dart';
import 'cancel_trip_sheet.dart';
import 'map_overlay.dart';
import 'trip_summary_screen.dart';

/// A trip the driver has: first the way to the pickup, then the way to the destination.
///
/// The driver's position comes from the home screen (which keeps reporting it to the
/// platform). This screen follows the trip's status every three seconds, records a point of
/// the route every twenty while the trip is in progress, and lets the driver start, finish
/// or cancel it, or raise an alert.
class ActiveTripScreen extends ConsumerStatefulWidget {
  final Trip trip;

  const ActiveTripScreen({super.key, required this.trip});

  @override
  ConsumerState<ActiveTripScreen> createState() => _ActiveTripScreenState();
}

class _ActiveTripScreenState extends ConsumerState<ActiveTripScreen> {
  late Trip _trip;

  Timer? _tripTimer;
  Timer? _waypointTimer;
  bool _pollingTrip = false;
  bool _leaving = false;
  bool _busy = false;

  MapOverlay? _overlay;
  bool _styleReady = false;

  bool _routing = false;
  bool _fitted = false;
  String _routeFor = '';
  DateTime? _routeAt;
  double? _routeMeters;
  String _eta = '';

  @override
  void initState() {
    super.initState();
    _trip = widget.trip;

    _tripTimer = Timer.periodic(const Duration(seconds: 3), (_) => _pollTrip());
    _waypointTimer = Timer.periodic(const Duration(seconds: 20), (_) => _sendWaypoint());
  }

  @override
  void dispose() {
    _stopTimers();
    super.dispose();
  }

  void _stopTimers() {
    _tripTimer?.cancel();
    _waypointTimer?.cancel();
  }

  bool get _started => _trip.status == TripStatus.inProgress;

  GeoPoint get _target => _started ? _trip.dropoff : _trip.pickup;

  Future<void> _pollTrip() async {
    if (_pollingTrip || _leaving || !mounted) return;
    _pollingTrip = true;

    try {
      final trip = await ref.read(tripApiProvider).getTrip(_trip.id);
      if (!mounted || _leaving) return;

      switch (trip.status) {
        case TripStatus.completed:
          _leaving = true;
          _stopTimers();
          Navigator.of(context).pushReplacement(
            MaterialPageRoute(builder: (_) => TripSummaryScreen(trip: trip)),
          );
        case TripStatus.cancelled:
          _leaving = true;
          _stopTimers();
          await showDialog<void>(
            context: context,
            barrierDismissible: false,
            builder: (dialogContext) => AlertDialog(
              title: const Text('أُلغيت الرحلة'),
              content: const Text('ألغى الراكب هذه الرحلة.'),
              actions: [
                TextButton(
                  onPressed: () => Navigator.of(dialogContext).pop(),
                  child: const Text('حسناً'),
                ),
              ],
            ),
          );
          _goHome();
        case TripStatus.requested:
        case TripStatus.accepted:
        case TripStatus.inProgress:
        case TripStatus.unknown:
          if (trip.status != _trip.status) {
            setState(() {
              _trip = trip;
              _fitted = false;
              _routeAt = null;
            });
            _drawFixedPoints();
          }
      }
    } on ApiException {
      // A dropped connection is not the end of the trip: the next poll tries again.
    } finally {
      _pollingTrip = false;
    }
  }

  Future<void> _sendWaypoint() async {
    if (!_started || _leaving) return;

    final position = ref.read(driverPositionProvider);
    if (position == null) return;

    try {
      await ref.read(tripApiProvider).recordWaypoint(_trip.id, position);
    } on ApiException {
      // The route record is not worth interrupting the trip for.
    }
  }

  void _drawFixedPoints() {
    final overlay = _overlay;
    if (overlay == null || !_styleReady) return;

    overlay.setPickup(_started ? null : _trip.pickup);
    overlay.setDropoff(_trip.dropoff);
  }

  void _onPosition(GeoPoint position) {
    _overlay?.setDriver(position);
    _refreshRoute(position);
  }

  /// The road from the driver to where they are heading, redrawn about every 30 seconds.
  Future<void> _refreshRoute(GeoPoint driver) async {
    final overlay = _overlay;
    if (_routing || overlay == null || !_styleReady) return;

    final key = _trip.status.name;
    final asked = _routeAt;
    if (asked != null && _routeFor == key && DateTime.now().difference(asked) < const Duration(seconds: 30)) {
      return;
    }

    _routing = true;

    try {
      final route = await ref.read(mapsApiProvider).route(driver, _target);
      if (!mounted) return;

      setState(() {
        _routeFor = key;
        _routeAt = DateTime.now();
        _routeMeters = route.distanceMeters;
        _eta = formatMinutes(route.durationSeconds);
      });

      await overlay.setRoute(route.path);

      if (!_fitted) {
        _fitted = true;
        await overlay.fit([driver, _target], bottomPadding: 380);
      }
    } on ApiException {
      // The line is a nicety: the driver still sees the dots.
    } finally {
      _routing = false;
    }
  }

  Future<bool> _confirm({required String title, required String body, required String action}) async {
    final answer = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(title),
        content: Text(body),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('تراجع'),
          ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: Text(action),
          ),
        ],
      ),
    );

    return answer == true;
  }

  Future<void> _start() async {
    if (_busy) return;

    final position = ref.read(driverPositionProvider);
    if (position != null) {
      final away = position.distanceMetersTo(_trip.pickup);

      if (away > 300) {
        final go = await _confirm(
          title: 'لسا ما وصلت لنقطة الانطلاق',
          body: 'أنت تبعد عنها ${arabicDigits((away / 1000).toStringAsFixed(1))} كم. تبدأ الرحلة؟',
          action: 'ابدأ',
        );
        if (!go || !mounted) return;
      }
    }

    setState(() => _busy = true);

    try {
      final trip = await ref.read(tripApiProvider).startTrip(_trip.id);
      if (!mounted) return;

      setState(() {
        _trip = trip;
        _busy = false;
        _fitted = false;
        _routeAt = null;
      });
      _drawFixedPoints();
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() => _busy = false);
      _tell(describeFailure(error, wrong: 'تعذر بدء الرحلة. ممكن الراكب ألغاها'));
    }
  }

  Future<void> _complete() async {
    if (_busy) return;

    final position = ref.read(driverPositionProvider);
    final awayMeters = position?.distanceMetersTo(_trip.dropoff) ?? 0;

    final finish = await _confirm(
      title: 'إنهاء الرحلة؟',
      body: awayMeters > 500
          ? 'أنت تبعد عن الوجهة ${arabicDigits((awayMeters / 1000).toStringAsFixed(1))} كم. سنحسب الأجرة عند الإنهاء.'
          : 'هل وصلت للوجهة؟ سنحسب الأجرة عند الإنهاء.',
      action: 'إنهاء',
    );
    if (!finish || !mounted) return;

    setState(() => _busy = true);

    try {
      final trip = await ref.read(tripApiProvider).completeTrip(_trip.id);
      if (!mounted) return;

      _leaving = true;
      _stopTimers();
      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => TripSummaryScreen(trip: trip)),
      );
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() => _busy = false);
      _tell(describeFailure(error, wrong: 'تعذر إنهاء الرحلة. حاول مرة أخرى'));
    }
  }

  Future<void> _cancel() async {
    if (_busy) return;

    final reason = await showCancelTripSheet(context);
    if (reason == null || !mounted) return;

    setState(() => _busy = true);

    try {
      await ref.read(tripApiProvider).cancelTrip(_trip.id, reason);
      if (!mounted) return;

      _leaving = true;
      _stopTimers();
      _goHome();
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() => _busy = false);
      _tell(describeFailure(error, wrong: 'تعذر إلغاء الرحلة. حاول مرة أخرى'));
    }
  }

  /// The emergency button: asks first, tells the safety team with the driver's position,
  /// then offers to call the emergency number.
  Future<void> _sos() async {
    final confirmed = await _confirm(
      title: 'طلب مساعدة عاجلة',
      body: 'سنرسل تنبيهاً لفريق السلامة مع موقعك. استخدمه فقط إذا كنت بحاجة لمساعدة.',
      action: 'إرسال التنبيه',
    );
    if (!confirmed || !mounted) return;

    try {
      await ref.read(tripApiProvider).triggerSos(
            tripId: _trip.id,
            location: ref.read(driverPositionProvider) ?? _trip.pickup,
          );
    } on ApiException {
      if (!mounted) return;
      _tell('تعذر إرسال التنبيه. اتصل بالطوارئ ${AppConfig.emergencyNumber} مباشرة');
      return;
    }

    if (!mounted) return;

    final call = await _confirm(
      title: 'وصل تنبيهك',
      body: 'أُبلغ فريق السلامة. هل تريد الاتصال بالطوارئ (${AppConfig.emergencyNumber}) الآن؟',
      action: 'اتصل الآن',
    );

    if (call) {
      await launchUrl(Uri(scheme: 'tel', path: AppConfig.emergencyNumber));
    }
  }

  void _goHome() {
    if (!mounted) return;
    Navigator.of(context).popUntil((route) => route.isFirst);
  }

  void _tell(String message) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    ref.listen<GeoPoint?>(driverPositionProvider, (previous, next) {
      if (next != null) _onPosition(next);
    });

    final pickupLabel = ref.watch(placeLabelProvider(_trip.pickup)).valueOrNull ?? '...';
    final dropoffLabel = ref.watch(placeLabelProvider(_trip.dropoff)).valueOrNull ?? '...';
    final meters = _routeMeters;

    return PopScope(
      canPop: false,
      child: Scaffold(
        backgroundColor: colors.surface100,
        body: Stack(
          children: [
            Positioned.fill(
              child: MapLibreMap(
                styleString: kMapStyleUrl,
                initialCameraPosition: CameraPosition(
                  target: LatLng(_trip.pickup.latitude, _trip.pickup.longitude),
                  zoom: 13,
                ),
                onMapCreated: (controller) => _overlay = MapOverlay(controller),
                onStyleLoadedCallback: () {
                  _styleReady = true;
                  _drawFixedPoints();

                  final position = ref.read(driverPositionProvider);
                  if (position != null) _onPosition(position);
                },
              ),
            ),
            Align(
              alignment: Alignment.bottomCenter,
              child: Container(
                width: double.infinity,
                padding: const EdgeInsets.fromLTRB(AppSpacing.space4, AppSpacing.space5, AppSpacing.space4, AppSpacing.space5 + AppSpacing.space1),
                decoration: BoxDecoration(
                  color: colors.surface200,
                  borderRadius: const BorderRadius.only(topLeft: Radius.circular(AppRadius.md), topRight: Radius.circular(AppRadius.md)),
                  boxShadow: [BoxShadow(color: colors.ink.withOpacity(0.08), blurRadius: 16, offset: const Offset(0, -2))],
                ),
                child: SafeArea(
                  top: false,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Text(
                        _started ? 'الرحلة جارية، توجّه إلى الوجهة' : 'توجّه إلى نقطة الانطلاق',
                        style: textTheme.titleMedium,
                      ),
                      if (_eta.isNotEmpty && meters != null) ...[
                        const SizedBox(height: AppSpacing.space1),
                        Text(
                          '$_eta · ${arabicDigits((meters / 1000).toStringAsFixed(1))} كم',
                          style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                        ),
                      ],
                      const SizedBox(height: AppSpacing.space3),
                      Row(
                        children: [
                          Icon(_started ? Icons.place_outlined : Icons.circle, size: _started ? 18 : 10, color: _started ? colors.brand500 : colors.success),
                          const SizedBox(width: AppSpacing.space2),
                          Expanded(
                            child: Text(
                              _started ? dropoffLabel : pickupLabel,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: textTheme.bodyLarge,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: AppSpacing.space2),
                      Text(
                        _trip.paymentMethod == 'wallet' ? 'الدفع: من محفظة الراكب (وأي فرق نقداً)' : 'الدفع: نقداً',
                        style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                      ),
                      const SizedBox(height: AppSpacing.space4),
                      ElevatedButton(
                        onPressed: _busy ? null : (_started ? _complete : _start),
                        child: _busy
                            ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                            : Text(_started ? 'إنهاء الرحلة' : 'بدء الرحلة'),
                      ),
                      const SizedBox(height: AppSpacing.space2),
                      Row(
                        children: [
                          Expanded(
                            child: OutlinedButton.icon(
                              onPressed: _sos,
                              style: OutlinedButton.styleFrom(
                                foregroundColor: colors.danger,
                                side: BorderSide(color: colors.danger),
                              ),
                              icon: const Icon(Icons.warning_amber_rounded, size: 18),
                              label: const Text('طوارئ'),
                            ),
                          ),
                          if (!_started) ...[
                            const SizedBox(width: AppSpacing.space3),
                            Expanded(
                              child: TextButton(
                                onPressed: _busy ? null : _cancel,
                                style: TextButton.styleFrom(foregroundColor: colors.danger),
                                child: const Text('إلغاء الرحلة'),
                              ),
                            ),
                          ],
                        ],
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
