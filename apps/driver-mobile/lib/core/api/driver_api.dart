import '../models/driver_profile.dart';
import '../models/geo_point.dart';
import 'api_client.dart';
import 'api_exception.dart';

/// The driver routes of the gateway: the profile, going online and the live position.
class DriverApi {
  DriverApi(this._client);

  final ApiClient _client;

  /// The driver profile of this identity, or null if the person has not registered yet.
  ///
  /// [signOutOnExpiry] false is for the check made when the app opens.
  Future<DriverProfile?> getByIdentity(
    String identityId, {
    bool signOutOnExpiry = true,
  }) async {
    try {
      final json = await _client.get(
        '/v1/identities/$identityId/driver',
        signOutOnExpiry: signOutOnExpiry,
      );

      return _profileOf(json);
    } on ApiException catch (error) {
      if (error.isNotFound) {
        return null;
      }

      rethrow;
    }
  }

  Future<DriverProfile?> get(String driverId) async {
    final json = await _client.get('/v1/drivers/$driverId');

    return _profileOf(json);
  }

  /// Registers a new driver. The account starts as pending: an operator has to approve it
  /// before the driver can go online. If the profile already exists (a repeated tap, a
  /// second phone) the existing one is returned.
  Future<DriverProfile> create({
    required String identityId,
    required String displayName,
    required Vehicle vehicle,
  }) async {
    try {
      final json = await _client.post(
        '/v1/drivers',
        body: <String, dynamic>{
          'identityId': identityId,
          'displayName': displayName,
          'vehicle': vehicle.toJson(),
        },
      );

      final profile = _profileOf(json);
      if (profile == null) {
        throw const ApiException(message: 'the backend returned no driver profile');
      }

      return profile;
    } on ApiException catch (error) {
      if (!error.isConflict) {
        rethrow;
      }

      final existing = await getByIdentity(identityId);
      if (existing == null) {
        rethrow;
      }

      return existing;
    }
  }

  /// Goes online (available for trips) or offline. The backend refuses it for an account
  /// that is not active, or one that is suspended for what it owes.
  Future<DriverProfile?> setOnline(String driverId, {required bool online}) async {
    final json = await _client.put(
      '/v1/drivers/$driverId/availability',
      body: <String, dynamic>{
        'availabilityStatus':
            online ? 'AVAILABILITY_STATUS_AVAILABLE' : 'AVAILABILITY_STATUS_OFFLINE',
      },
    );

    return _profileOf(json);
  }

  /// Tells the platform where the driver is. A position is forgotten after 30 seconds, so
  /// while online this is sent every few seconds.
  Future<void> reportLocation(String driverId, GeoPoint position) async {
    await _client.put(
      '/v1/locations/$driverId',
      body: <String, dynamic>{
        'entityType': 'ENTITY_TYPE_DRIVER',
        'coordinates': position.toJson(),
      },
    );
  }

  Future<DriverStanding> standing(String driverId) async {
    final json = await _client.get('/v1/drivers/$driverId/standing');

    return DriverStanding.fromJson(json);
  }

  DriverProfile? _profileOf(Map<String, dynamic> json) {
    final driver = json['driver'];

    if (driver is Map<String, dynamic>) {
      return DriverProfile.fromJson(driver);
    }

    if (driver is Map) {
      return DriverProfile.fromJson(Map<String, dynamic>.from(driver));
    }

    return null;
  }
}
