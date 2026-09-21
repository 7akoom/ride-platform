import 'package:maplibre_gl/maplibre_gl.dart';

import '../../core/models/geo_point.dart';

LatLng toLatLng(GeoPoint point) => LatLng(point.latitude, point.longitude);

/// The dots and the line the app draws on top of the map: pickup, destination, the
/// driver, and the route between them.
///
/// The map can be gone or its style not loaded yet when a drawing call arrives (the
/// screen was left, the phone was slow), so every call swallows that failure instead of
/// crashing the trip screen.
class MapOverlay {
  MapOverlay(this._controller);

  final MapLibreMapController _controller;

  Circle? _pickup;
  Circle? _dropoff;
  Circle? _driver;
  Line? _route;

  Future<void> setPickup(GeoPoint? point) async {
    _pickup = await _dot(_pickup, point, '#2F8F4E');
  }

  Future<void> setDropoff(GeoPoint? point) async {
    _dropoff = await _dot(_dropoff, point, '#0E7C7B');
  }

  Future<void> setDriver(GeoPoint? point) async {
    _driver = await _dot(_driver, point, '#1B2430', radius: 10);
  }

  Future<void> setRoute(List<GeoPoint> path) async {
    try {
      final old = _route;
      if (old != null) {
        await _controller.removeLine(old);
        _route = null;
      }

      if (path.length < 2) {
        return;
      }

      _route = await _controller.addLine(
        LineOptions(
          geometry: path.map(toLatLng).toList(),
          lineColor: '#0E7C7B',
          lineWidth: 5.0,
          lineOpacity: 0.85,
        ),
      );
    } catch (_) {
      // See the class comment.
    }
  }

  /// Moves the camera so all [points] are visible above the bottom sheet.
  Future<void> fit(List<GeoPoint> points, {double bottomPadding = 340}) async {
    if (points.isEmpty) {
      return;
    }

    try {
      if (points.length == 1) {
        await _controller.animateCamera(
          CameraUpdate.newLatLngZoom(toLatLng(points.first), 15),
        );
        return;
      }

      var south = points.first.latitude;
      var north = points.first.latitude;
      var west = points.first.longitude;
      var east = points.first.longitude;

      for (final point in points) {
        if (point.latitude < south) south = point.latitude;
        if (point.latitude > north) north = point.latitude;
        if (point.longitude < west) west = point.longitude;
        if (point.longitude > east) east = point.longitude;
      }

      await _controller.animateCamera(
        CameraUpdate.newLatLngBounds(
          LatLngBounds(
            southwest: LatLng(south, west),
            northeast: LatLng(north, east),
          ),
          left: 60,
          top: 120,
          right: 60,
          bottom: bottomPadding,
        ),
      );
    } catch (_) {
      // See the class comment.
    }
  }

  Future<Circle?> _dot(
    Circle? existing,
    GeoPoint? point,
    String color, {
    double radius = 8,
  }) async {
    try {
      if (point == null) {
        if (existing != null) {
          await _controller.removeCircle(existing);
        }
        return null;
      }

      final position = toLatLng(point);

      if (existing != null) {
        await _controller.updateCircle(existing, CircleOptions(geometry: position));
        return existing;
      }

      return await _controller.addCircle(
        CircleOptions(
          geometry: position,
          circleRadius: radius,
          circleColor: color,
          circleStrokeColor: '#FFFFFF',
          circleStrokeWidth: 3,
        ),
      );
    } catch (_) {
      return existing;
    }
  }
}
