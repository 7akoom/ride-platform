import 'dart:math' as math;

/// A position on the map.
class GeoPoint {
  final double latitude;
  final double longitude;

  const GeoPoint(this.latitude, this.longitude);

  factory GeoPoint.fromJson(Object? json) {
    if (json is Map) {
      final lat = json['latitude'];
      final lng = json['longitude'];

      return GeoPoint(
        lat is num ? lat.toDouble() : 0,
        lng is num ? lng.toDouble() : 0,
      );
    }

    return const GeoPoint(0, 0);
  }

  Map<String, dynamic> toJson() => <String, dynamic>{
        'latitude': latitude,
        'longitude': longitude,
      };

  /// True for the (0, 0) the backend sends when a position is missing.
  bool get isEmpty => latitude == 0 && longitude == 0;

  /// The distance to [other] in metres, along the surface of the Earth (haversine).
  double distanceMetersTo(GeoPoint other) {
    const earthRadius = 6371000.0;

    double radians(double degrees) => degrees * math.pi / 180;

    final dLat = radians(other.latitude - latitude);
    final dLng = radians(other.longitude - longitude);
    final a = math.pow(math.sin(dLat / 2), 2) +
        math.cos(radians(latitude)) * math.cos(radians(other.latitude)) * math.pow(math.sin(dLng / 2), 2);

    return 2 * earthRadius * math.asin(math.min(1.0, math.sqrt(a)));
  }

  @override
  bool operator ==(Object other) =>
      other is GeoPoint && other.latitude == latitude && other.longitude == longitude;

  @override
  int get hashCode => Object.hash(latitude, longitude);
}
