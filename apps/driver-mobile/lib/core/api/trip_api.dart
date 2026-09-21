import '../models/geo_point.dart';
import '../models/trip.dart';
import 'api_client.dart';
import 'api_exception.dart';

/// The trip routes of the gateway, for the driver.
class TripApi {
  TripApi(this._client);

  final ApiClient _client;

  /// The driver's accepted or in-progress trip, or null if there is none. (Trips are handed
  /// to the driver directly unless the platform is set to offers.)
  Future<Trip?> activeTrip(String driverId) async {
    try {
      final json = await _client.get(
        '/v1/trips:active',
        query: <String, dynamic>{'driverId': driverId},
      );

      return _tripOf(json);
    } on ApiException catch (error) {
      if (error.isNotFound) {
        return null;
      }

      rethrow;
    }
  }

  /// The trip currently offered to this driver, or null if there is none.
  Future<TripOffer?> pendingOffer(String driverId) async {
    try {
      final json = await _client.get('/v1/drivers/$driverId/offer');
      final offer = json['offer'];

      if (offer is Map) {
        return TripOffer.fromJson(Map<String, dynamic>.from(offer));
      }

      return null;
    } on ApiException catch (error) {
      if (error.isNotFound) {
        return null;
      }

      rethrow;
    }
  }

  /// Takes the offered trip. Throws [ApiException]: 404 when there is no live offer, 400 when
  /// it expired, the trip was cancelled, or the driver is on another trip.
  Future<Trip> acceptOffer({required String tripId, required String driverId}) async {
    final json = await _client.post(
      '/v1/trips/$tripId:accept-offer',
      body: <String, dynamic>{'driverId': driverId},
    );

    return _tripOf(json);
  }

  /// Turns the offer down: the trip goes on to the next driver.
  Future<void> rejectOffer({required String tripId, required String driverId}) async {
    await _client.post(
      '/v1/trips/$tripId:reject-offer',
      body: <String, dynamic>{'driverId': driverId},
    );
  }

  Future<Trip> getTrip(String tripId) async {
    final json = await _client.get('/v1/trips/$tripId');

    return _tripOf(json);
  }

  Future<Trip> startTrip(String tripId) async {
    final json = await _client.post('/v1/trips/$tripId:start');

    return _tripOf(json);
  }

  Future<Trip> completeTrip(String tripId) async {
    final json = await _client.post('/v1/trips/$tripId:complete');

    return _tripOf(json);
  }

  Future<Trip> cancelTrip(String tripId, String reason) async {
    final json = await _client.post(
      '/v1/trips/$tripId:cancel',
      body: <String, dynamic>{'reason': reason},
    );

    return _tripOf(json);
  }

  /// A point of the route driven, for the trip's record. The backend keeps one every few
  /// seconds at most and refuses the rest; that is not an error worth acting on.
  Future<void> recordWaypoint(String tripId, GeoPoint position) async {
    await _client.post(
      '/v1/trips/$tripId:waypoint',
      body: <String, dynamic>{'location': position.toJson()},
    );
  }

  /// Raises a safety alert for the trip.
  Future<void> triggerSos({required String tripId, required GeoPoint location}) async {
    await _client.post(
      '/v1/trips/$tripId:sos',
      body: <String, dynamic>{
        'triggeredBy': 'SOS_TRIGGERED_BY_DRIVER',
        'location': location.toJson(),
      },
    );
  }

  /// The driver's rating of the rider. Stars are 1 to 5.
  Future<void> rateRider({required String tripId, required int stars, String comment = ''}) async {
    await _client.post(
      '/v1/trips/$tripId:rate',
      body: <String, dynamic>{
        'ratedBy': 'RATED_BY_DRIVER',
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
