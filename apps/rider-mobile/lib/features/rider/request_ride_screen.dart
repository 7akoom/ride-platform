import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maplibre_gl/maplibre_gl.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../core/format.dart';
import '../../core/map_config.dart';
import '../../core/models/fare.dart';
import '../../core/models/geo_point.dart';
import '../../core/models/place.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../state/home_events.dart';
import '../../state/lookups.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';
import '../support/support_screen.dart';
import '../wallet/wallet_home_screen.dart';
import 'map_overlay.dart';
import 'map_picker_screen.dart';
import 'notifications_screen.dart';
import 'profile_screen.dart';
import 'ride_history_screen.dart';
import 'searching_driver_screen.dart';
import 'set_destination_screen.dart';
import 'settings_screen.dart';
import 'tracking_screen.dart';

class RequestRideScreen extends ConsumerStatefulWidget {
  const RequestRideScreen({super.key});

  @override
  ConsumerState<RequestRideScreen> createState() => _RequestRideScreenState();
}

class _RequestRideScreenState extends ConsumerState<RequestRideScreen> {
  final _scaffoldKey = GlobalKey<ScaffoldState>();

  MapOverlay? _overlay;
  bool _styleReady = false;

  // Where the trip starts. It follows the phone's position until the rider moves the pin.
  GeoPoint _pickup = kErbilCenter;
  String _pickupLabel = 'موقعك الحالي';
  bool _pickupChosenByHand = false;
  bool _gotPhonePosition = false;

  PickedPlace? _destination;
  RouteInfo? _route;
  FareEstimate? _fare;
  String? _estimateError;
  bool _estimating = false;
  int _estimateToken = 0;

  String _payment = 'cash';
  String? _walletBalance;
  String _currency = 'IQD';

  bool _requesting = false;
  String? _requestError;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _loadWallet();
      _resumeActiveTrip();
    });
  }

  /// A trip the rider already has (the app was closed during it) is picked up again.
  Future<void> _resumeActiveTrip() async {
    try {
      final riderId = await SessionStorage.readRiderId();
      if (riderId == null) return;

      final trip = await ref.read(tripApiProvider).activeTrip(riderId);
      if (trip != null && mounted) {
        _openTrip(trip, label: '');
      }
    } on ApiException {
      // Not being able to check is not worth interrupting the rider for.
    }
  }

  Future<void> _loadWallet() async {
    try {
      final riderId = await SessionStorage.readRiderId();
      if (riderId == null) return;

      final wallet = await ref.read(moneyApiProvider).walletBalance(riderId);
      if (!mounted) return;
      setState(() {
        _walletBalance = wallet.balance;
        if (wallet.currencyCode.isNotEmpty) _currency = wallet.currencyCode;
      });
    } on ApiException {
      // The payment choice still works without showing the balance.
    }
  }

  void _openTrip(Trip trip, {required String label}) {
    final Widget screen = trip.status == TripStatus.requested
        ? SearchingDriverScreen(trip: trip, destinationLabel: label)
        : TrackingScreen(trip: trip, destinationLabel: label);

    Navigator.of(context).push(MaterialPageRoute(builder: (_) => screen));
  }

  void _onPhonePosition(UserLocation location) {
    if (_gotPhonePosition || _pickupChosenByHand) return;

    _gotPhonePosition = true;
    _pickup = GeoPoint(location.position.latitude, location.position.longitude);
    _overlay?.fit([_pickup], bottomPadding: 300);

    if (_destination != null) {
      _estimate();
    }
  }

  Future<void> _pickPickup() async {
    final picked = await Navigator.of(context).push<PickedPlace>(
      MaterialPageRoute(
        builder: (_) => MapPickerScreen(title: 'حدد نقطة الانطلاق', initial: _pickup),
      ),
    );
    if (picked == null || !mounted) return;

    setState(() {
      _pickup = picked.point;
      _pickupLabel = picked.label;
      _pickupChosenByHand = true;
    });

    if (_destination != null) {
      _estimate();
    } else {
      _overlay?.fit([_pickup], bottomPadding: 300);
    }
  }

  Future<void> _pickDestination() async {
    final picked = await Navigator.of(context).push<PickedPlace>(
      MaterialPageRoute(builder: (_) => SetDestinationScreen(near: _pickup)),
    );
    if (picked == null || !mounted) return;

    setState(() {
      _destination = picked;
      _requestError = null;
    });
    _estimate();
  }

  /// The road, the time and the price for the chosen pickup and destination.
  Future<void> _estimate() async {
    final destination = _destination;
    if (destination == null) return;

    final token = ++_estimateToken;
    setState(() {
      _estimating = true;
      _estimateError = null;
      _route = null;
      _fare = null;
    });

    try {
      final riderId = await SessionStorage.readRiderId();
      if (riderId == null) {
        throw const ApiException(statusCode: 401, message: 'there is no rider profile');
      }

      final route = await ref.read(mapsApiProvider).route(_pickup, destination.point);
      final fare = await ref.read(moneyApiProvider).estimateFare(
            riderId: riderId,
            pickup: _pickup,
            dropoff: destination.point,
          );

      if (!mounted || token != _estimateToken) return;
      setState(() {
        _route = route;
        _fare = fare;
        _estimating = false;
      });

      await _redraw();
      await _overlay?.fit([_pickup, destination.point]);
    } on ApiException catch (error) {
      if (!mounted || token != _estimateToken) return;
      setState(() {
        _estimating = false;
        _estimateError = _estimateMessage(error);
      });
      await _redraw();
    }
  }

  String _estimateMessage(ApiException error) {
    if (error.message.contains('service zone')) {
      return 'موقع الانطلاق خارج مناطق الخدمة حالياً';
    }

    return describeFailure(error, wrong: 'ما قدرنا نحسب الرحلة بين هالنقطتين. جرّب وجهة ثانية');
  }

  Future<void> _requestRide() async {
    final destination = _destination;
    final fare = _fare;
    if (destination == null || fare == null || _requesting) return;

    setState(() {
      _requesting = true;
      _requestError = null;
    });

    try {
      final riderId = await SessionStorage.readRiderId();
      if (riderId == null) {
        throw const ApiException(statusCode: 401, message: 'there is no rider profile');
      }

      final trip = await ref.read(tripApiProvider).requestTrip(
            riderId: riderId,
            pickup: _pickup,
            dropoff: destination.point,
            paymentMethod: _payment,
          );

      if (!mounted) return;
      setState(() => _requesting = false);
      _openTrip(trip, label: destination.label);
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() => _requesting = false);

      // The rider already has a trip running (another phone, a closed app): go to it.
      if (error.message.contains('active trip')) {
        await _resumeActiveTrip();
        return;
      }

      setState(() {
        _requestError = error.message.contains('service zone')
            ? 'موقع الانطلاق خارج مناطق الخدمة حالياً'
            : describeFailure(error, wrong: 'تعذر طلب الرحلة. حاول مرة أخرى');
      });
    }
  }

  /// A trip ended: the old destination and price are no longer wanted.
  void _resetAfterTrip() {
    setState(() {
      _destination = null;
      _route = null;
      _fare = null;
      _estimateError = null;
      _requestError = null;
    });

    _redraw();
    _loadWallet();
  }

  Future<void> _redraw() async {
    final overlay = _overlay;
    if (overlay == null || !_styleReady) return;

    final destination = _destination;

    await overlay.setPickup(destination == null && !_pickupChosenByHand ? null : _pickup);
    await overlay.setDropoff(destination?.point);
    await overlay.setRoute(_route?.path ?? const <GeoPoint>[]);
  }

  String? _walletHint() {
    final fare = _fare;
    if (_payment != 'wallet' || fare == null) return null;

    final balance = double.tryParse(_walletBalance ?? '') ?? 0;
    final total = double.tryParse(fare.total) ?? 0;

    if (balance >= total) return 'سيُخصم المبلغ من محفظتك';
    if (balance <= 0) return 'محفظتك فارغة، فسيُدفع المبلغ كله نقداً للسائق';

    final rest = total - balance;

    return 'المحفظة تغطي ${formatMoney('$balance', fare.currencyCode)} والباقي ${formatMoney('$rest', fare.currencyCode)} نقداً للسائق';
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final destination = _destination;
    final fare = _fare;
    final route = _route;
    final walletHint = _walletHint();

    ref.listen<HomeNotice?>(homeNoticeProvider, (previous, next) {
      if (next == null) return;

      ref.read(homeNoticeProvider.notifier).state = null;
      _resetAfterTrip();

      if (next.message.isNotEmpty) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(next.message)));
      }
    });

    return Scaffold(
      key: _scaffoldKey,
      backgroundColor: colors.surface100,
      drawer: _AppDrawer(colors: colors, textTheme: textTheme),
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
              onMapCreated: (controller) => _overlay = MapOverlay(controller),
              onStyleLoadedCallback: () {
                _styleReady = true;
                _redraw();
              },
              onUserLocationUpdated: _onPhonePosition,
            ),
          ),
          Positioned(
            top: AppSpacing.space4,
            left: AppSpacing.space4,
            child: SafeArea(
              child: _RoundIconButton(
                icon: Icons.menu,
                onPressed: () => _scaffoldKey.currentState?.openDrawer(),
              ),
            ),
          ),
          Align(
            alignment: Alignment.bottomCenter,
            child: Container(
              width: double.infinity,
              padding: const EdgeInsets.fromLTRB(
                AppSpacing.space4,
                AppSpacing.space5,
                AppSpacing.space4,
                AppSpacing.space5 + AppSpacing.space1,
              ),
              decoration: BoxDecoration(
                color: colors.surface200,
                borderRadius: const BorderRadius.only(
                  topLeft: Radius.circular(AppRadius.md),
                  topRight: Radius.circular(AppRadius.md),
                ),
                boxShadow: [
                  BoxShadow(color: colors.ink.withOpacity(0.08), blurRadius: 16, offset: const Offset(0, -2)),
                ],
              ),
              child: SafeArea(
                top: false,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text('إلى أين؟', style: textTheme.titleLarge),
                    const SizedBox(height: AppSpacing.space4),
                    _LocationRow(
                      icon: Icons.circle,
                      iconColor: colors.success,
                      caption: 'من',
                      value: _pickupLabel,
                      onTap: _pickPickup,
                      colors: colors,
                      textTheme: textTheme,
                    ),
                    const SizedBox(height: AppSpacing.space2),
                    _LocationRow(
                      icon: Icons.place_outlined,
                      iconColor: colors.brand500,
                      caption: destination == null ? null : 'إلى',
                      value: destination?.label ?? 'ابحث عن وجهة',
                      muted: destination == null,
                      onTap: _pickDestination,
                      colors: colors,
                      textTheme: textTheme,
                    ),
                    if (destination != null) ...[
                      const SizedBox(height: AppSpacing.space3),
                      if (_estimating)
                        Row(
                          children: [
                            const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2)),
                            const SizedBox(width: AppSpacing.space2),
                            Text('جاري حساب الأجرة...', style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                          ],
                        )
                      else if (_estimateError != null)
                        Text(_estimateError!, style: textTheme.bodySmall?.copyWith(color: colors.danger))
                      else if (fare != null && route != null) ...[
                        Text(
                          '${formatMinutes(route.durationSeconds)} · ${formatMoney(fare.total, fare.currencyCode)} تقريباً',
                          style: textTheme.titleMedium,
                        ),
                        const SizedBox(height: AppSpacing.space3),
                        Row(
                          children: [
                            Expanded(
                              child: _PaymentChip(
                                label: 'نقداً',
                                selected: _payment == 'cash',
                                colors: colors,
                                onTap: () => setState(() => _payment = 'cash'),
                              ),
                            ),
                            const SizedBox(width: AppSpacing.space3),
                            Expanded(
                              child: _PaymentChip(
                                label: _walletBalance == null
                                    ? 'المحفظة'
                                    : 'المحفظة · ${formatMoney(_walletBalance!, _currency)}',
                                selected: _payment == 'wallet',
                                colors: colors,
                                onTap: () => setState(() => _payment = 'wallet'),
                              ),
                            ),
                          ],
                        ),
                        if (walletHint != null) ...[
                          const SizedBox(height: AppSpacing.space2),
                          Text(walletHint, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                        ],
                      ],
                    ],
                    if (_requestError != null) ...[
                      const SizedBox(height: AppSpacing.space2),
                      Text(_requestError!, style: textTheme.bodySmall?.copyWith(color: colors.danger)),
                    ],
                    const SizedBox(height: AppSpacing.space4),
                    ElevatedButton(
                      onPressed: fare != null && !_requesting ? _requestRide : null,
                      style: ElevatedButton.styleFrom(
                        backgroundColor: fare != null ? colors.brand500 : colors.inkMuted,
                        disabledBackgroundColor: colors.inkMuted,
                      ),
                      child: _requesting
                          ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                          : const Text('اطلب رحلة'),
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

class _AppDrawer extends ConsumerWidget {
  final AppColors colors;
  final TextTheme textTheme;

  const _AppDrawer({required this.colors, required this.textTheme});

  void _go(BuildContext context, Widget screen) {
    Navigator.of(context).pop();
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => screen));
  }

  /// Saved addresses, payment requests and coupons have no backend yet.
  void _soon(BuildContext context) {
    Navigator.of(context).pop();
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('هذه الميزة قريباً')),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final profile = ref.watch(riderProfileProvider).valueOrNull;
    final unread = ref.watch(notificationsProvider).valueOrNull?.unread ?? 0;

    return Drawer(
      backgroundColor: colors.surface200,
      child: SafeArea(
        child: ListView(
          padding: EdgeInsets.zero,
          children: [
            Padding(
              padding: const EdgeInsets.all(AppSpacing.space4),
              child: Row(
                children: [
                  CircleAvatar(radius: 22, backgroundColor: colors.brand100, child: Text(profile?.initial ?? '', style: TextStyle(color: colors.brand600))),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(child: Text(profile?.displayName ?? '', style: textTheme.titleMedium, overflow: TextOverflow.ellipsis)),
                ],
              ),
            ),
            Divider(color: colors.border, height: 1),
            ListTile(leading: Icon(Icons.history, color: colors.ink), title: Text('رحلاتي', style: textTheme.bodyLarge), onTap: () => _go(context, const RideHistoryScreen())),
            ListTile(leading: Icon(Icons.person_outline, color: colors.ink), title: Text('الملف الشخصي', style: textTheme.bodyLarge), onTap: () => _go(context, const ProfileScreen())),
            ListTile(leading: Badge(label: Text('$unread'), isLabelVisible: unread > 0, child: Icon(Icons.notifications_outlined, color: colors.ink)), title: Text('الإشعارات', style: textTheme.bodyLarge), onTap: () => _go(context, const NotificationsScreen())),
            ListTile(leading: Icon(Icons.place_outlined, color: colors.ink), title: Text('عناويني المحفوظة', style: textTheme.bodyLarge), onTap: () => _soon(context)),
            ListTile(leading: Icon(Icons.account_balance_wallet_outlined, color: colors.ink), title: Text('المحفظة', style: textTheme.bodyLarge), onTap: () => _go(context, const WalletHomeScreen())),
            ListTile(leading: Icon(Icons.swap_horiz, color: colors.ink), title: Text('طلبات الدفع', style: textTheme.bodyLarge), onTap: () => _soon(context)),
            ListTile(leading: Icon(Icons.local_offer_outlined, color: colors.ink), title: Text('الكوبونات', style: textTheme.bodyLarge), onTap: () => _soon(context)),
            ListTile(leading: Icon(Icons.settings_outlined, color: colors.ink), title: Text('الإعدادات', style: textTheme.bodyLarge), onTap: () => _go(context, const SettingsScreen())),
            ListTile(leading: Icon(Icons.help_outline, color: colors.ink), title: Text('المساعدة والدعم', style: textTheme.bodyLarge), onTap: () => _go(context, const SupportScreen())),
          ],
        ),
      ),
    );
  }
}

class _RoundIconButton extends StatelessWidget {
  final IconData icon;
  final VoidCallback onPressed;

  const _RoundIconButton({required this.icon, required this.onPressed});

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.white,
      shape: const CircleBorder(),
      elevation: 3,
      child: IconButton(icon: Icon(icon, color: const Color(0xFF1B2430)), onPressed: onPressed),
    );
  }
}

class _LocationRow extends StatelessWidget {
  final IconData icon;
  final Color iconColor;
  final String? caption;
  final String value;
  final bool muted;
  final VoidCallback onTap;
  final AppColors colors;
  final TextTheme textTheme;

  const _LocationRow({
    required this.icon,
    required this.iconColor,
    required this.value,
    required this.onTap,
    required this.colors,
    required this.textTheme,
    this.caption,
    this.muted = false,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.sm),
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space3 + 2, vertical: AppSpacing.space3),
        decoration: BoxDecoration(
          color: colors.surface200,
          border: Border.all(color: colors.border),
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Row(
          children: [
            Icon(icon, size: icon == Icons.circle ? 12 : 18, color: iconColor),
            const SizedBox(width: AppSpacing.space2 + 2),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (caption != null)
                    Text(caption!, style: textTheme.labelLarge?.copyWith(color: colors.inkMuted, fontWeight: FontWeight.w400)),
                  Text(
                    value,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: muted ? textTheme.bodyLarge?.copyWith(color: colors.inkMuted) : textTheme.titleMedium,
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _PaymentChip extends StatelessWidget {
  final String label;
  final bool selected;
  final AppColors colors;
  final VoidCallback onTap;

  const _PaymentChip({
    required this.label,
    required this.selected,
    required this.colors,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.sm),
      child: Container(
        height: 44,
        alignment: Alignment.center,
        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space2),
        decoration: BoxDecoration(
          color: selected ? colors.brand100 : colors.surface200,
          border: Border.all(color: selected ? colors.brand500 : colors.border, width: 1.5),
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Text(
          label,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(fontWeight: FontWeight.w600, fontSize: 13, color: colors.ink),
        ),
      ),
    );
  }
}
