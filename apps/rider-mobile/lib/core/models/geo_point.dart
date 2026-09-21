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

  @override
  bool operator ==(Object other) =>
      other is GeoPoint && other.latitude == latitude && other.longitude == longitude;

  @override
  int get hashCode => Object.hash(latitude, longitude);
}
