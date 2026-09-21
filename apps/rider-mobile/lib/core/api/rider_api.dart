import '../models/rider_profile.dart';
import 'api_client.dart';
import 'api_exception.dart';

/// The rider routes of the gateway.
class RiderApi {
  RiderApi(this._client);

  final ApiClient _client;

  /// The rider profile of this identity, or null if the person has not made one yet.
  ///
  /// [signOutOnExpiry] false is for the check made when the app opens.
  Future<RiderProfile?> getByIdentity(
    String identityId, {
    bool signOutOnExpiry = true,
  }) async {
    try {
      final json = await _client.get(
        '/v1/identities/$identityId/rider',
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

  /// Makes the profile for a new rider. If it already exists (a repeated tap, a second
  /// phone) the existing one is returned.
  Future<RiderProfile> create({
    required String identityId,
    required String displayName,
  }) async {
    try {
      final json = await _client.post(
        '/v1/riders',
        body: <String, dynamic>{
          'identityId': identityId,
          'displayName': displayName,
        },
      );

      final profile = _profileOf(json);
      if (profile == null) {
        throw const ApiException(message: 'the backend returned no rider profile');
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

  /// Changes the rider's name.
  Future<void> rename({required String riderId, required String displayName}) async {
    await _client.patch(
      '/v1/riders/$riderId',
      body: <String, dynamic>{'displayName': displayName},
    );
  }

  RiderProfile? _profileOf(Map<String, dynamic> json) {
    final rider = json['rider'];

    if (rider is Map<String, dynamic>) {
      return RiderProfile.fromJson(rider);
    }

    if (rider is Map) {
      return RiderProfile.fromJson(Map<String, dynamic>.from(rider));
    }

    return null;
  }
}
