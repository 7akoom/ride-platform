import 'dart:async';

import 'package:geolocator/geolocator.dart';

import '../core/models/geo_point.dart';

/// Why the phone's position could not be used.
enum LocationStartResult { ok, serviceDisabled, denied, deniedForever }

/// Follows the phone's GPS and keeps telling the platform where the driver is.
///
/// The platform forgets a position 30 seconds after the last report, so the last known
/// position is sent every five seconds whether the driver moved or not, and a moving driver
/// is followed by the GPS stream.
///
/// It works while the app is open on screen (the screen is kept on while online). Keeping
/// it going with the screen off or the app in the background needs a foreground service
/// with a notification; that is a later step.
class LocationReporter {
  StreamSubscription<Position>? _subscription;
  Timer? _timer;
  Future<void> Function(GeoPoint position)? _upload;
  GeoPoint? _last;

  bool get running => _subscription != null;

  /// The last position seen, or null before the first fix.
  GeoPoint? get last => _last;

  /// Asks for the location permission if it was not asked yet, and says whether the phone's
  /// position can be used.
  static Future<LocationStartResult> ensurePermission() async {
    if (!await Geolocator.isLocationServiceEnabled()) {
      return LocationStartResult.serviceDisabled;
    }

    var permission = await Geolocator.checkPermission();

    if (permission == LocationPermission.denied) {
      permission = await Geolocator.requestPermission();
    }

    if (permission == LocationPermission.denied) {
      return LocationStartResult.denied;
    }

    if (permission == LocationPermission.deniedForever) {
      return LocationStartResult.deniedForever;
    }

    return LocationStartResult.ok;
  }

  /// Starts following the GPS. [onPosition] is called with every new position; [upload]
  /// sends one to the platform.
  Future<LocationStartResult> start({
    required void Function(GeoPoint position) onPosition,
    required Future<void> Function(GeoPoint position) upload,
  }) async {
    final permission = await ensurePermission();
    if (permission != LocationStartResult.ok) {
      return permission;
    }

    stop();
    _upload = upload;

    _subscription = Geolocator.getPositionStream(
      locationSettings: const LocationSettings(
        accuracy: LocationAccuracy.high,
        distanceFilter: 10,
      ),
    ).listen(
      (position) {
        final point = GeoPoint(position.latitude, position.longitude);
        _last = point;
        onPosition(point);
      },
      onError: (Object _) {
        // A GPS hiccup: the next fix or the next tick carries on.
      },
    );

    // The phone usually knows roughly where it is already: use that until the first fix.
    try {
      final known = await Geolocator.getLastKnownPosition();
      if (known != null && _last == null) {
        final point = GeoPoint(known.latitude, known.longitude);
        _last = point;
        onPosition(point);
      }
    } catch (_) {
      // Not available on every platform.
    }

    _timer = Timer.periodic(const Duration(seconds: 5), (_) => _push());
    await _push();

    return LocationStartResult.ok;
  }

  Future<void> _push() async {
    final point = _last;
    final upload = _upload;
    if (point == null || upload == null) return;

    try {
      await upload(point);
    } catch (_) {
      // The next tick tries again. A dropped connection must not stop the reporting.
    }
  }

  void stop() {
    _subscription?.cancel();
    _subscription = null;
    _timer?.cancel();
    _timer = null;
    _upload = null;
    _last = null;
  }
}

/// What to tell the driver when the position cannot be used.
String locationMessage(LocationStartResult result) {
  switch (result) {
    case LocationStartResult.ok:
      return '';
    case LocationStartResult.serviceDisabled:
      return 'شغّل خدمة الموقع (GPS) بجهازك حتى تقدر تستقبل الرحلات';
    case LocationStartResult.denied:
      return 'نحتاج إذن الموقع حتى نوصلك الرحلات القريبة منك';
    case LocationStartResult.deniedForever:
      return 'إذن الموقع مرفوض. فعّله من إعدادات التطبيق بالجهاز';
  }
}
