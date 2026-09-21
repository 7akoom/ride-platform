import '../models/fare.dart';
import '../models/geo_point.dart';
import '../models/wallet_tx.dart';
import 'api_client.dart';
import 'api_exception.dart';

/// Fare estimates and the rider's wallet.
class MoneyApi {
  MoneyApi(this._client);

  final ApiClient _client;

  /// What the trip is expected to cost. Throws [ApiException] (400) when the pickup is
  /// outside every service zone.
  Future<FareEstimate> estimateFare({
    required String riderId,
    required GeoPoint pickup,
    required GeoPoint dropoff,
    String vehicleClass = 'economy',
  }) async {
    final json = await _client.post(
      '/v1/fare-estimates',
      body: <String, dynamic>{
        'riderId': riderId,
        'pickup': pickup.toJson(),
        'dropoff': dropoff.toJson(),
        'vehicleClass': vehicleClass,
      },
    );

    final fare = json['fare'];
    if (fare is! Map) {
      throw const ApiException(message: 'the backend returned no fare');
    }

    return FareEstimate(
      total: fare['total'] as String? ?? '0',
      currencyCode: fare['currencyCode'] as String? ?? '',
    );
  }

  /// The rider's wallet balance as a decimal string, and its currency.
  Future<({String balance, String currencyCode})> walletBalance(String riderId) async {
    final json = await _client.get(
      '/v1/wallets/$riderId',
      query: <String, dynamic>{'ownerType': 'OWNER_TYPE_RIDER'},
    );

    final wallet = json['wallet'];

    if (wallet is Map) {
      return (
        balance: wallet['balance'] as String? ?? '0',
        currencyCode: wallet['currencyCode'] as String? ?? '',
      );
    }

    return (balance: '0', currencyCode: '');
  }

  /// The rider's wallet history, newest first.
  Future<List<WalletTx>> transactions(String riderId, {int limit = 50}) async {
    final json = await _client.get(
      '/v1/wallets/$riderId/transactions',
      query: <String, dynamic>{'ownerType': 'OWNER_TYPE_RIDER', 'limit': limit},
    );

    final items = json['transactions'];

    return <WalletTx>[
      if (items is List)
        for (final item in items)
          if (item is Map) WalletTx.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  /// How a finished trip was paid, or null until the trip has been settled (a few seconds
  /// after it ends).
  Future<TripSettlement?> tripSettlement({
    required String riderId,
    required String tripId,
  }) async {
    try {
      final json = await _client.get(
        '/v1/wallets/$riderId/trips/$tripId/settlement',
        query: <String, dynamic>{'ownerType': 'OWNER_TYPE_RIDER'},
      );

      return TripSettlement.fromJson(json);
    } on ApiException catch (error) {
      if (error.isNotFound) {
        return null;
      }

      rethrow;
    }
  }
}
