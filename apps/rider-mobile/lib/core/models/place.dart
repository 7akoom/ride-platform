import 'geo_point.dart';

/// A place found by name or by pointing at the map.
class Place {
  final String id;
  final String name;
  final String displayName;
  final GeoPoint point;

  const Place({
    required this.id,
    required this.name,
    required this.displayName,
    required this.point,
  });

  factory Place.fromJson(Map<String, dynamic> json) {
    return Place(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      displayName: json['displayName'] as String? ?? '',
      point: GeoPoint.fromJson(json['coordinates']),
    );
  }

  /// The short name, or the start of the long one if there is no short name.
  String get label {
    if (name.isNotEmpty) {
      return name;
    }

    return displayName.split(',').first.trim();
  }
}

/// A place the rider chose: where to go, or where to be picked up.
class PickedPlace {
  final String label;
  final String sub;
  final GeoPoint point;

  const PickedPlace({required this.label, required this.sub, required this.point});
}

/// The best road between two points.
class RouteInfo {
  final double distanceMeters;
  final double durationSeconds;

  /// The path to draw, decoded from the backend's encoded polyline.
  final List<GeoPoint> path;

  const RouteInfo({
    required this.distanceMeters,
    required this.durationSeconds,
    required this.path,
  });
}
