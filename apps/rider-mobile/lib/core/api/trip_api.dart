import '../models/geo_point.dart';
import '../models/trip.dart';
import 'api_client.dart';
import 'api_exception.dart';

/// The trip routes of the gateway, for the rider.
class TripApi {
  TripApi(this._client);

  final ApiClient _client;

  /// Asks for a trip. [paymentMethod] is 'cash' or 'wallet' and cannot change later.
  Future<Trip> requestTrip({
    required String riderId,
    required GeoPoint pickup,
    required GeoPoint dropoff,
    required String paymentMethod,
    String vehicleClass = 'economy',
  }) async {
    final json = await _client.post(
      '/v1/trips',
      body: <String, dynamic>{
        'riderId': riderId,
        'pickup': pickup.toJson(),
        'dropoff': dropoff.toJson(),
        'vehicleClass': vehicleClass,
        'paymentMethod': paymentMethod,
      },
    );

    return _tripOf(json);
  }

  Future<Trip> getTrip(String tripId) async {
    final json = await _client.get('/v1/trips/$tripId');

    return _tripOf(json);
  }

  Future<Trip> cancelTrip(String tripId, String reason) async {
    final json = await _client.post(
      '/v1/trips/$tripId:cancel',
      body: <String, dynamic>{'reason': reason},
    );

    return _tripOf(json);
  }

  /// The rider's requested, accepted or in-progress trip, or null if there is none.
  Future<Trip?> activeTrip(String riderId) async {
    try {
      final json = await _client.get(
        '/v1/trips:active',
        query: <String, dynamic>{'riderId': riderId},
      );

      return _tripOf(json);
    } on ApiException catch (error) {
      if (error.isNotFound) {
        return null;
      }

      rethrow;
    }
  }

  /// One page of the rider's trips, newest first. Pass the previous page's
  /// [TripPage.nextPageToken] for the next page.
  Future<TripPage> listTrips(
    String riderId, {
    String pageToken = '',
    int pageSize = 20,
  }) async {
    final json = await _client.get(
      '/v1/trips',
      query: <String, dynamic>{
        'riderId': riderId,
        'pageSize': pageSize,
        if (pageToken.isNotEmpty) 'pageToken': pageToken,
      },
    );

    final items = json['trips'];

    return TripPage(
      trips: <Trip>[
        if (items is List)
          for (final item in items)
            if (item is Map) Trip.fromJson(Map<String, dynamic>.from(item)),
      ],
      nextPageToken: json['nextPageToken'] as String? ?? '',
    );
  }

  /// Where the driver last reported being, or null if they have not reported lately (the
  /// app keeps asking).
  Future<GeoPoint?> driverLocation(String tripId) async {
    try {
      final json = await _client.get('/v1/trips/$tripId/driver-location');
      final point = GeoPoint.fromJson(json['location']);

      return point.isEmpty ? null : point;
    } on ApiException catch (error) {
      if (error.isNotFound || error.statusCode == 400) {
        return null;
      }

      rethrow;
    }
  }

  /// The driver's name, car and rating, or null while the backend cannot say.
  Future<TripDriverInfo?> tripDriver(String tripId) async {
    try {
      final json = await _client.get('/v1/trips/$tripId/driver');
      final driver = json['driver'];

      if (driver is Map) {
        return TripDriverInfo.fromJson(Map<String, dynamic>.from(driver));
      }

      return null;
    } on ApiException catch (error) {
      if (error.isNetwork || error.isUnauthorized) {
        rethrow;
      }

      return null;
    }
  }

  /// Raises a safety alert for the trip: the operating company's safety team is told, with
  /// the position given here.
  Future<void> triggerSos({required String tripId, required GeoPoint location}) async {
    await _client.post(
      '/v1/trips/$tripId:sos',
      body: <String, dynamic>{
        'triggeredBy': 'SOS_TRIGGERED_BY_RIDER',
        'location': location.toJson(),
      },
    );
  }

  /// The rider's rating of the driver. Stars are 1 to 5.
  Future<void> rateDriver({
    required String tripId,
    required int stars,
    String comment = '',
  }) async {
    await _client.post(
      '/v1/trips/$tripId:rate',
      body: <String, dynamic>{
        'ratedBy': 'RATED_BY_RIDER',
        'stars': stars,
        'comment': comment,
      },
    );
  }

  Trip _tripOf(Map<String, dynamic> json) {
    final trip = json['trip'];

    if (trip is Map) {
      return Trip.fromJson(Map<String, dynamic>.from(trip));
    }

    throw const ApiException(message: 'the backend returned no trip');
  }
}
