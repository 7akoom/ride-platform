import '../geo/polyline.dart';
import '../models/geo_point.dart';
import '../models/place.dart';
import 'api_client.dart';
import 'api_exception.dart';

/// Routes and places: the map routes of the gateway.
class MapsApi {
  MapsApi(this._client);

  final ApiClient _client;

  /// The best road between two points. Throws [ApiException] when there is none (404) or a
  /// point is not near a road (400).
  Future<RouteInfo> route(GeoPoint origin, GeoPoint destination) async {
    final json = await _client.post(
      '/v1/routes:compute',
      body: <String, dynamic>{
        'origin': origin.toJson(),
        'destination': destination.toJson(),
      },
    );

    final polyline = json['polyline'];

    return RouteInfo(
      distanceMeters: (json['distanceMeters'] as num?)?.toDouble() ?? 0,
      durationSeconds: (json['durationSeconds'] as num?)?.toDouble() ?? 0,
      path: polyline is String ? decodePolyline(polyline) : const <GeoPoint>[],
    );
  }

  /// Places matching what was typed, best first. [near] ranks close places first.
  Future<List<Place>> searchPlaces(
    String query, {
    GeoPoint? near,
    int limit = 8,
    String language = 'ar',
  }) async {
    final json = await _client.get(
      '/v1/places:search',
      query: <String, dynamic>{
        'query': query,
        'limit': limit,
        'language': language,
        if (near != null) 'near.latitude': near.latitude,
        if (near != null) 'near.longitude': near.longitude,
      },
    );

    final places = json['places'];
    if (places is! List) {
      return const <Place>[];
    }

    return <Place>[
      for (final item in places)
        if (item is Map) Place.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  /// What is at a point, or null when there is nothing there.
  Future<Place?> reverse(GeoPoint point, {String language = 'ar'}) async {
    try {
      final json = await _client.get(
        '/v1/places:reverse',
        query: <String, dynamic>{
          'coordinates.latitude': point.latitude,
          'coordinates.longitude': point.longitude,
          'language': language,
        },
      );

      final place = json['place'];

      return place is Map ? Place.fromJson(Map<String, dynamic>.from(place)) : null;
    } on ApiException catch (error) {
      if (error.isNotFound) {
        return null;
      }

      rethrow;
    }
  }
}
