import '../models/settlement.dart';
import 'api_client.dart';
import 'api_exception.dart';

/// What the driver earned from a trip, and the change they could not give.
class MoneyApi {
  MoneyApi(this._client);

  final ApiClient _client;

  /// How a finished trip was settled, or null until it has been (a few seconds after it ends).
  Future<DriverSettlement?> tripSettlement({
    required String driverId,
    required String tripId,
  }) async {
    try {
      final json = await _client.get(
        '/v1/wallets/$driverId/trips/$tripId/settlement',
        query: <String, dynamic>{'ownerType': 'OWNER_TYPE_DRIVER'},
      );

      return DriverSettlement.fromJson(json);
    } on ApiException catch (error) {
      if (error.isNotFound) {
        return null;
      }

      rethrow;
    }
  }

  /// Tells the platform the rider paid [cashReceived] for a trip that cost less, and the
  /// driver has no change: the difference is credited to the rider's wallet, not taken from
  /// the driver. Once per trip, within a small limit set by the platform (400 above it).
  /// Returns the change credited, as a decimal string.
  Future<String> recordChange({
    required String driverId,
    required String tripId,
    required int cashReceived,
  }) async {
    final json = await _client.post(
      '/v1/drivers/$driverId/trips/$tripId/change',
      body: <String, dynamic>{'cashReceived': '$cashReceived'},
    );

    return json['changeAmount'] as String? ?? '0';
  }
}
