import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maplibre_gl/maplibre_gl.dart';
import 'package:wakelock_plus/wakelock_plus.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../core/format.dart';
import '../../core/map_config.dart';
import '../../core/models/driver_profile.dart';
import '../../core/models/geo_point.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../state/location_reporter.dart';
import '../../state/lookups.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';
import '../trip/active_trip_screen.dart';
import '../trip/offer_screen.dart';

/// The driver's home: the map, and one big button to start or stop working.
///
/// While online the phone's position is reported to the platform every few seconds and the
/// screen stays on. Every two seconds the screen also asks whether a trip has been given to
/// the driver (or offered to them), and opens it.
class DriverHomeScreen extends ConsumerStatefulWidget {
  const DriverHomeScreen({super.key});

  @override
  ConsumerState<DriverHomeScreen> createState() => _DriverHomeScreenState();
}

class _DriverHomeScreenState extends ConsumerState<DriverHomeScreen> {
  final _scaffoldKey = GlobalKey<ScaffoldState>();
  final _reporter = LocationReporter();

  MapLibreMapController? _map;
  GeoPoint? _position;
  bool _centered = false;

  bool _online = false;
  bool _busy = false;
  String? _message;

  Timer? _pollTimer;
  bool _polling = false;

  /// True from the moment an offer or a trip screen is opened until the driver is back on
  /// this screen: no new offer is looked for meanwhile.
  bool _inTrip = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _prepare());
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    _reporter.stop();
    WakelockPlus.disable();
    super.dispose();
  }

  /// Asks for the location permission (so the map shows where the driver is) and puts the
  /// backend in step with this screen.
  Future<void> _prepare() async {
    final permission = await LocationReporter.ensurePermission();
    if (!mounted) return;

    if (permission != LocationStartResult.ok) {
      setState(() => _message = locationMessage(permission));
    }

    try {
      final driverId = await SessionStorage.readDriverId();
      if (driverId == null) return;

      final api = ref.read(driverApiProvider);

      // The app was closed during a trip: pick it up again, and go back to reporting the
      // position (the driver is still online, and busy).
      final active = await ref.read(tripApiProvider).activeTrip(driverId);
      if (active != null) {
        final resumed = await _startReporting(driverId);
        if (resumed && mounted) {
          setState(() => _online = true);
          _startPolling();
          await _openActiveTrip(active);
        }
        return;
      }

      final profile = await api.get(driverId);

      // The app was closed while the driver was online: nothing reports a position any more,
      // so the backend must not keep counting them as available. (A driver who is busy with
      // a trip is left alone.)
      if (profile != null && profile.availability == DriverAvailability.available) {
        await api.setOnline(driverId, online: false);
      }
    } on ApiException {
      // Not being able to check is not worth interrupting the driver for.
    }
  }

  /// Starts following the GPS and reporting it, and keeps the screen on. Returns whether it
  /// worked.
  Future<bool> _startReporting(String driverId) async {
    final api = ref.read(driverApiProvider);

    final result = await _reporter.start(
      onPosition: _onPosition,
      upload: (position) => api.reportLocation(driverId, position),
    );

    if (result != LocationStartResult.ok) {
      if (mounted) setState(() => _message = locationMessage(result));
      return false;
    }

    await WakelockPlus.enable();

    return true;
  }

  void _startPolling() {
    _pollTimer?.cancel();
    _pollTimer = Timer.periodic(const Duration(seconds: 2), (_) => _poll());
  }

  void _stopPolling() {
    _pollTimer?.cancel();
    _pollTimer = null;
  }

  /// Looks for a trip given to the driver, and for an offer.
  Future<void> _poll() async {
    if (!_online || _polling || !mounted) return;

    if (_inTrip) {
      // If this screen is showing again, the trip screens are gone.
      if (ModalRoute.of(context)?.isCurrent ?? false) {
        _inTrip = false;
      } else {
        return;
      }
    }

    _polling = true;

    try {
      final driverId = await SessionStorage.readDriverId();
      if (driverId == null || !mounted) return;

      final trips = ref.read(tripApiProvider);

      final active = await trips.activeTrip(driverId);
      if (active != null && mounted) {
        await _openActiveTrip(active);
        return;
      }

      final offer = await trips.pendingOffer(driverId);
      if (offer != null && mounted) {
        await _openOffer(offer);
      }
    } on ApiException {
      // A dropped connection: the next poll tries again.
    } finally {
      _polling = false;
    }
  }

  Future<void> _openOffer(TripOffer offer) async {
    _inTrip = true;

    final trip = await Navigator.of(context).push<Trip>(
      MaterialPageRoute(builder: (_) => OfferScreen(offer: offer)),
    );

    if (trip != null && mounted) {
      await _openActiveTrip(trip);
    }
  }

  Future<void> _openActiveTrip(Trip trip) async {
    _inTrip = true;

    await Navigator.of(context).push<void>(
      MaterialPageRoute(builder: (_) => ActiveTripScreen(trip: trip)),
    );
  }

  void _onPosition(GeoPoint position) {
    _position = position;
    ref.read(driverPositionProvider.notifier).state = position;

    final map = _map;
    if (!_centered && map != null) {
      _centered = true;
      map.animateCamera(
        CameraUpdate.newLatLngZoom(LatLng(position.latitude, position.longitude), 15),
      );
    }
  }

  Future<void> _toggle() async {
    if (_busy) return;

    setState(() {
      _busy = true;
      _message = null;
    });

    try {
      if (_online) {
        await _goOffline();
      } else {
        await _goOnline();
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _goOnline() async {
    final driverId = await SessionStorage.readDriverId();
    if (driverId == null) {
      setState(() => _message = 'تعذر تحديد حسابك. سجّل الخروج وادخل من جديد');
      return;
    }

    final api = ref.read(driverApiProvider);

    if (!await _startReporting(driverId)) {
      return;
    }

    try {
      await api.setOnline(driverId, online: true);

      if (!mounted) return;
      setState(() => _online = true);
      _startPolling();
    } on ApiException catch (error) {
      _reporter.stop();
      await WakelockPlus.disable();

      if (!mounted) return;
      setState(() {
        _message = describeFailure(
          error,
          wrong: 'تعذر بدء العمل. تأكد أن حسابك مفعّل وأن ما عليك مبالغ مستحقة',
        );
      });
    }
  }

  /// Tells the backend first: if that fails the driver stays online (and keeps reporting),
  /// so they are never shown as offline while trips can still be offered to them.
  Future<void> _goOffline() async {
    final driverId = await SessionStorage.readDriverId();

    if (driverId != null) {
      try {
        await ref.read(driverApiProvider).setOnline(driverId, online: false);
      } on ApiException catch (error) {
        if (!mounted) return;
        setState(() {
          _message = describeFailure(error, wrong: 'تعذر إيقاف العمل. حاول مرة أخرى');
        });
        return;
      }
    }

    _stopPolling();
    _reporter.stop();
    await WakelockPlus.disable();

    if (!mounted) return;
    setState(() => _online = false);
  }

  Future<void> _signOut() async {
    if (_online) {
      try {
        await _goOffline();
      } catch (_) {
        // Signing out must work even if going offline could not be confirmed.
      }
    }

    _stopPolling();
    _reporter.stop();
    await WakelockPlus.disable();

    if (!mounted) return;
    await signOut(ref);
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final standing = ref.watch(standingProvider).valueOrNull;
    final blocked = standing != null && !standing.canTakeTrips;
    final amountDue = standing?.amountDue ?? '0';

    return Scaffold(
      key: _scaffoldKey,
      backgroundColor: colors.surface100,
      drawer: _DriverDrawer(colors: colors, textTheme: textTheme, onSignOut: _signOut),
      body: Stack(
        children: [
          Positioned.fill(
            child: MapLibreMap(
              styleString: kMapStyleUrl,
              initialCameraPosition: const CameraPosition(
                target: LatLng(36.1911, 44.0092), // Erbil
                zoom: 13,
              ),
              myLocationEnabled: true,
              onMapCreated: (controller) {
                _map = controller;

                final known = _position;
                if (known != null && !_centered) {
                  _centered = true;
                  controller.animateCamera(
                    CameraUpdate.newLatLngZoom(LatLng(known.latitude, known.longitude), 15),
                  );
                }
              },
            ),
          ),
          Positioned(
            top: AppSpacing.space4,
            left: AppSpacing.space4,
            child: SafeArea(
              child: Material(
                color: colors.surface200,
                shape: const CircleBorder(),
                elevation: 3,
                child: IconButton(
                  icon: Icon(Icons.menu, color: colors.ink),
                  onPressed: () => _scaffoldKey.currentState?.openDrawer(),
                ),
              ),
            ),
          ),
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
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Container(
                        width: 8,
                        height: 8,
                        decoration: BoxDecoration(color: _online ? colors.success : colors.inkMuted, shape: BoxShape.circle),
                      ),
                      const SizedBox(width: AppSpacing.space2),
                      Text(_online ? 'متصل' : 'غير متصل', style: textTheme.labelLarge),
                    ],
                  ),
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
                    Text(
                      _online ? 'بانتظار طلبات الرحلات' : 'ابدأ العمل حتى تصلك الرحلات',
                      style: textTheme.titleMedium,
                    ),
                    if (_online) ...[
                      const SizedBox(height: AppSpacing.space1),
                      Text(
                        'أبقِ التطبيق مفتوحاً والشاشة شغّالة أثناء العمل',
                        style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                      ),
                    ],
                    if (blocked) ...[
                      const SizedBox(height: AppSpacing.space3),
                      Container(
                        padding: const EdgeInsets.all(AppSpacing.space3),
                        decoration: BoxDecoration(color: colors.surface100, borderRadius: BorderRadius.circular(AppRadius.sm), border: Border.all(color: colors.danger)),
                        child: Text(
                          'لا يمكنك استقبال الرحلات حالياً. المبلغ المستحق عليك: ${formatMoney(amountDue, 'IQD')}',
                          style: textTheme.bodySmall?.copyWith(color: colors.danger),
                        ),
                      ),
                    ],
                    if (_message != null) ...[
                      const SizedBox(height: AppSpacing.space3),
                      Text(_message!, style: textTheme.bodySmall?.copyWith(color: colors.danger)),
                    ],
                    const SizedBox(height: AppSpacing.space4),
                    ElevatedButton(
                      onPressed: _busy ? null : _toggle,
                      style: ElevatedButton.styleFrom(
                        backgroundColor: _online ? colors.danger : colors.brand500,
                      ),
                      child: _busy
                          ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                          : Text(_online ? 'إيقاف العمل' : 'ابدأ العمل'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _DriverDrawer extends ConsumerWidget {
  final AppColors colors;
  final TextTheme textTheme;
  final VoidCallback onSignOut;

  const _DriverDrawer({
    required this.colors,
    required this.textTheme,
    required this.onSignOut,
  });

  void _soon(BuildContext context) {
    Navigator.of(context).pop();
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('هذه الميزة قريباً')),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final profile = ref.watch(driverProfileProvider).valueOrNull;

    return Drawer(
      backgroundColor: colors.surface200,
      child: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.all(AppSpacing.space5),
              child: Row(
                children: [
                  CircleAvatar(
                    radius: 24,
                    backgroundColor: colors.brand100,
                    child: Text(profile?.initial ?? '', style: TextStyle(color: colors.brand600, fontWeight: FontWeight.w600)),
                  ),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(profile?.displayName ?? '', style: textTheme.titleMedium, overflow: TextOverflow.ellipsis),
                        if (profile != null)
                          Row(
                            children: [
                              Icon(Icons.star, size: 14, color: colors.warning),
                              const SizedBox(width: 4),
                              Text(arabicDigits(profile.ratingAverage.toStringAsFixed(1)), style: textTheme.bodySmall),
                            ],
                          ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            if (profile != null && profile.vehicle.summary.isNotEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5),
                child: Text(profile.vehicle.summary, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
              ),
            const SizedBox(height: AppSpacing.space3),
            Divider(height: 1, color: colors.border),
            ListTile(leading: Icon(Icons.payments_outlined, color: colors.ink), title: Text('الأرباح', style: textTheme.bodyLarge), onTap: () => _soon(context)),
            ListTile(leading: Icon(Icons.history, color: colors.ink), title: Text('رحلاتي', style: textTheme.bodyLarge), onTap: () => _soon(context)),
            const Spacer(),
            Divider(height: 1, color: colors.border),
            ListTile(
              leading: Icon(Icons.logout, color: colors.danger),
              title: Text('تسجيل الخروج', style: textTheme.bodyLarge?.copyWith(color: colors.danger)),
              onTap: onSignOut,
            ),
          ],
        ),
      ),
    );
  }
}
