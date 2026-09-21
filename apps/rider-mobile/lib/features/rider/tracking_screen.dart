import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maplibre_gl/maplibre_gl.dart';
import 'package:share_plus/share_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/api/api_exception.dart';
import '../../core/app_config.dart';
import '../../core/format.dart';
import '../../core/map_config.dart';
import '../../core/models/geo_point.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../state/home_events.dart';
import '../../theme/app_theme.dart';
import 'cancel_trip_sheet.dart';
import 'map_overlay.dart';
import 'trip_complete_screen.dart';

enum TripStage { accepted, enRoute, started }

/// Follows a trip a driver has accepted: the trip's status every three seconds, and the
/// driver's position every four, until the trip ends.
class TrackingScreen extends ConsumerStatefulWidget {
  final Trip trip;

  /// Where the trip goes ('' when not known).
  final String destinationLabel;

  const TrackingScreen({
    super.key,
    required this.trip,
    required this.destinationLabel,
  });

  @override
  ConsumerState<TrackingScreen> createState() => _TrackingScreenState();
}

class _TrackingScreenState extends ConsumerState<TrackingScreen> {
  late Trip _trip;

  TripDriverInfo? _driverInfo;
  int _driverInfoTries = 0;

  GeoPoint? _driverPoint;
  String _eta = '';
  DateTime? _etaAskedAt;

  Timer? _tripTimer;
  Timer? _locationTimer;
  bool _pollingTrip = false;
  bool _pollingLocation = false;
  bool _leaving = false;
  bool _cancelling = false;

  MapOverlay? _overlay;
  bool _styleReady = false;
  bool _routeDrawn = false;

  @override
  void initState() {
    super.initState();
    _trip = widget.trip;

    _tripTimer = Timer.periodic(const Duration(seconds: 3), (_) => _pollTrip());
    _locationTimer = Timer.periodic(const Duration(seconds: 4), (_) => _pollLocation());

    _loadDriverInfo();
    _pollLocation();
  }

  @override
  void dispose() {
    _tripTimer?.cancel();
    _locationTimer?.cancel();
    super.dispose();
  }

  TripStage get _stage {
    if (_trip.status == TripStatus.inProgress) return TripStage.started;
    if (_driverPoint != null) return TripStage.enRoute;

    return TripStage.accepted;
  }

  Future<void> _pollTrip() async {
    if (_pollingTrip || _leaving || !mounted) return;
    _pollingTrip = true;

    try {
      final trip = await ref.read(tripApiProvider).getTrip(_trip.id);
      if (!mounted || _leaving) return;

      switch (trip.status) {
        case TripStatus.completed:
          _stopTimers();
          _leaving = true;
          Navigator.of(context).pushReplacement(
            MaterialPageRoute(
              builder: (_) => TripCompleteScreen(trip: trip, destinationLabel: widget.destinationLabel),
            ),
          );
        case TripStatus.cancelled:
          _goHome('أُلغيت الرحلة');
        case TripStatus.requested:
        case TripStatus.accepted:
        case TripStatus.inProgress:
        case TripStatus.unknown:
          setState(() => _trip = trip);

          if (_driverInfo == null) {
            _loadDriverInfo();
          }
      }
    } on ApiException {
      // A dropped connection is not the end of the trip: the next poll tries again.
    } finally {
      _pollingTrip = false;
    }
  }

  /// The driver's name and car. It is asked for a few times only: if the backend cannot say,
  /// the card shows just "your driver".
  Future<void> _loadDriverInfo() async {
    if (_driverInfo != null || _driverInfoTries >= 3 || !_trip.hasDriver) return;
    _driverInfoTries++;

    try {
      final info = await ref.read(tripApiProvider).tripDriver(_trip.id);
      if (!mounted) return;
      setState(() => _driverInfo = info);
    } on ApiException {
      // Same as above.
    }
  }

  Future<void> _pollLocation() async {
    if (_pollingLocation || _leaving || !mounted || !_trip.hasDriver) return;
    if (_trip.status != TripStatus.accepted && _trip.status != TripStatus.inProgress) return;

    _pollingLocation = true;

    try {
      final point = await ref.read(tripApiProvider).driverLocation(_trip.id);
      if (!mounted || _leaving) return;

      if (point != null) {
        setState(() => _driverPoint = point);
        await _overlay?.setDriver(point);
        await _refreshEta(point);
      }
    } on ApiException {
      // Same as above.
    } finally {
      _pollingLocation = false;
    }
  }

  /// How long until the driver reaches the pickup (before the trip starts) or the
  /// destination (during it). Asked at most every 15 seconds.
  Future<void> _refreshEta(GeoPoint driver) async {
    final asked = _etaAskedAt;
    if (asked != null && DateTime.now().difference(asked) < const Duration(seconds: 15)) return;
    _etaAskedAt = DateTime.now();

    final target = _trip.status == TripStatus.inProgress ? _trip.dropoff : _trip.pickup;

    try {
      final route = await ref.read(mapsApiProvider).route(driver, target);
      if (!mounted) return;
      setState(() => _eta = formatMinutes(route.durationSeconds));
    } on ApiException {
      // The estimate is a nicety: the trip does not need it.
    }
  }

  Future<void> _drawTripOnMap() async {
    final overlay = _overlay;
    if (overlay == null || !_styleReady || _routeDrawn) return;
    _routeDrawn = true;

    await overlay.setPickup(_trip.pickup);
    await overlay.setDropoff(_trip.dropoff);
    await overlay.fit([_trip.pickup, _trip.dropoff], bottomPadding: 420);

    try {
      final route = await ref.read(mapsApiProvider).route(_trip.pickup, _trip.dropoff);
      await overlay.setRoute(route.path);
    } on ApiException {
      // The dots are enough without the line.
    }
  }

  void _stopTimers() {
    _tripTimer?.cancel();
    _locationTimer?.cancel();
  }

  void _goHome(String message) {
    if (_leaving) return;
    _leaving = true;
    _stopTimers();

    ref.read(homeNoticeProvider.notifier).state = HomeNotice(message);
    Navigator.of(context).popUntil((route) => route.isFirst);
  }

  Future<void> _cancel() async {
    if (_cancelling) return;

    final reason = await showCancelTripSheet(context);
    if (reason == null || !mounted) return;

    setState(() => _cancelling = true);

    try {
      await ref.read(tripApiProvider).cancelTrip(_trip.id, reason);
      if (!mounted) return;
      _goHome('تم إلغاء الرحلة');
    } on ApiException {
      if (!mounted) return;
      setState(() => _cancelling = false);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('تعذر إلغاء الرحلة. حاول مرة أخرى')),
      );
    }
  }

  /// The emergency button. It asks first (a tap by accident must not alert anyone), then
  /// tells the safety team with the position the app has (the driver's, once known: the
  /// rider is in the same car; the pickup before that) and offers to call the emergency
  /// number.
  Future<void> _sos() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('طلب مساعدة عاجلة'),
        content: const Text('سنرسل تنبيهاً لفريق السلامة مع موقع رحلتك. استخدمه فقط إذا كنت بحاجة لمساعدة.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('إلغاء'),
          ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            style: TextButton.styleFrom(foregroundColor: Colors.red),
            child: const Text('إرسال التنبيه'),
          ),
        ],
      ),
    );

    if (confirmed != true || !mounted) return;

    try {
      await ref.read(tripApiProvider).triggerSos(
            tripId: _trip.id,
            location: _driverPoint ?? _trip.pickup,
          );
    } on ApiException {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('تعذر إرسال التنبيه. اتصل بالطوارئ ${AppConfig.emergencyNumber} مباشرة')),
      );
      return;
    }

    if (!mounted) return;

    final call = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('وصل تنبيهك'),
        content: Text('أُبلغ فريق السلامة. هل تريد الاتصال بالطوارئ (${AppConfig.emergencyNumber}) الآن؟'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('لاحقاً'),
          ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('اتصل الآن'),
          ),
        ],
      ),
    );

    if (call == true) {
      await launchUrl(Uri(scheme: 'tel', path: AppConfig.emergencyNumber));
    }
  }

  void _share() {
    final label = widget.destinationLabel;
    final text = label.isEmpty ? 'أنا بالطريق برحلة عبر Ride Platform' : 'أنا بالطريق إلى $label برحلة عبر Ride Platform';

    SharePlus.instance.share(ShareParams(text: text));
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final stage = _stage;
    final info = _driverInfo;

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
                  _drawTripOnMap();
                },
              ),
            ),
            if (_eta.isNotEmpty)
              Positioned(
                top: AppSpacing.space4,
                left: 0,
                right: 0,
                child: SafeArea(
                  child: Center(
                    child: Container(
                      padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space4, vertical: AppSpacing.space2),
                      decoration: BoxDecoration(
                        color: colors.surface200,
                        borderRadius: BorderRadius.circular(AppRadius.full),
                        boxShadow: [BoxShadow(color: colors.ink.withOpacity(0.12), blurRadius: 8)],
                      ),
                      child: Text(_eta, style: textTheme.labelLarge),
                    ),
                  ),
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
                      Row(
                        children: [
                          Container(width: 8, height: 8, decoration: BoxDecoration(color: colors.success, shape: BoxShape.circle)),
                          const SizedBox(width: AppSpacing.space2),
                          Text(_statusText(stage), style: textTheme.titleMedium),
                        ],
                      ),
                      const SizedBox(height: AppSpacing.space4),
                      Row(
                        children: [
                          CircleAvatar(
                            radius: 26,
                            backgroundColor: colors.brand100,
                            child: info != null && info.initial.isNotEmpty
                                ? Text(info.initial, style: textTheme.titleLarge?.copyWith(color: colors.brand600, fontSize: 20))
                                : Icon(Icons.person, color: colors.brand600),
                          ),
                          const SizedBox(width: AppSpacing.space3),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(info != null && info.name.isNotEmpty ? info.name : 'سائقك', style: textTheme.titleMedium),
                                if (info != null && info.carInfo.isNotEmpty)
                                  Text(info.carInfo, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                              ],
                            ),
                          ),
                          if (info != null)
                            Container(
                              padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space2 + 2, vertical: AppSpacing.space1),
                              decoration: BoxDecoration(color: colors.surface100, borderRadius: BorderRadius.circular(AppRadius.full)),
                              child: Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  Icon(Icons.star, size: 12, color: colors.warning),
                                  const SizedBox(width: 4),
                                  Text(arabicDigits(info.ratingAverage.toStringAsFixed(1)), style: textTheme.labelLarge),
                                ],
                              ),
                            ),
                        ],
                      ),
                      const SizedBox(height: AppSpacing.space5),
                      _StageStepper(stage: stage, colors: colors),
                      const SizedBox(height: AppSpacing.space4),
                      OutlinedButton.icon(
                        onPressed: _sos,
                        style: OutlinedButton.styleFrom(
                          foregroundColor: colors.danger,
                          side: BorderSide(color: colors.danger),
                        ),
                        icon: const Icon(Icons.warning_amber_rounded, size: 18),
                        label: const Text('طوارئ'),
                      ),
                      TextButton.icon(
                        onPressed: _share,
                        style: TextButton.styleFrom(foregroundColor: colors.brand500),
                        icon: const Icon(Icons.share_outlined, size: 16),
                        label: const Text('مشاركة تفاصيل الرحلة'),
                      ),
                      if (stage != TripStage.started)
                        TextButton(
                          onPressed: _cancelling ? null : _cancel,
                          style: TextButton.styleFrom(foregroundColor: colors.danger),
                          child: const Text('إلغاء الرحلة'),
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

  String _statusText(TripStage stage) {
    switch (stage) {
      case TripStage.accepted:
        return 'تم قبول الطلب';
      case TripStage.enRoute:
        return 'السائق في الطريق إليك';
      case TripStage.started:
        return 'الرحلة جارية';
    }
  }
}

class _StageStepper extends StatelessWidget {
  final TripStage stage;
  final AppColors colors;

  const _StageStepper({required this.stage, required this.colors});

  @override
  Widget build(BuildContext context) {
    final labels = ['تم القبول', 'في الطريق', 'بدأت الرحلة'];
    final stageIndex = TripStage.values.indexOf(stage);

    return Row(
      children: List.generate(labels.length * 2 - 1, (i) {
        if (i.isOdd) {
          final leftDone = (i - 1) ~/ 2 < stageIndex;
          return Expanded(
            child: Container(
              height: 2,
              margin: const EdgeInsets.only(bottom: 20),
              color: leftDone ? colors.brand500 : colors.border,
            ),
          );
        }
        final idx = i ~/ 2;
        final isCurrent = idx == stageIndex;
        final isDone = idx < stageIndex;
        return Expanded(
          child: Column(
            children: [
              Container(
                width: isCurrent ? 12 : 10,
                height: isCurrent ? 12 : 10,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color:
                      (isCurrent || isDone) ? colors.brand500 : colors.border,
                  boxShadow: isCurrent
                      ? [
                          BoxShadow(
                            color: colors.brand100,
                            blurRadius: 0,
                            spreadRadius: 4,
                          ),
                        ]
                      : null,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                labels[idx],
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w600,
                  color: isCurrent ? colors.brand500 : colors.inkMuted,
                ),
              ),
            ],
          ),
        );
      }),
    );
  }
}
