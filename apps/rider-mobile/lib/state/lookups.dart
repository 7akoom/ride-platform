import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api/api_exception.dart';
import '../core/models/fare.dart';
import '../core/models/geo_point.dart';
import '../core/models/trip.dart';
import '../core/models/wallet_tx.dart';
import 'api_providers.dart';
import 'session_storage.dart';

/// The name of the place at a point, for showing a trip's start and end. Each point is
/// looked up once and remembered.
final placeLabelProvider = FutureProvider.family<String, GeoPoint>((ref, point) async {
  try {
    final place = await ref.read(mapsApiProvider).reverse(point);
    final label = place?.label ?? '';

    return label.isEmpty ? 'موقع على الخريطة' : label;
  } on ApiException {
    return 'موقع على الخريطة';
  }
});

/// How a trip was paid. It is asked again each time the list is opened until the trip has
/// been settled, then remembered.
final settlementProvider = FutureProvider.autoDispose.family<TripSettlement?, String>((ref, tripId) async {
  final riderId = await SessionStorage.readRiderId();
  if (riderId == null) {
    return null;
  }

  try {
    final settlement = await ref.read(moneyApiProvider).tripSettlement(
          riderId: riderId,
          tripId: tripId,
        );

    if (settlement != null) {
      ref.keepAlive();
    }

    return settlement;
  } on ApiException {
    return null;
  }
});

/// Who drove a trip (null if the backend cannot say).
final tripDriverProvider = FutureProvider.autoDispose.family<TripDriverInfo?, String>((ref, tripId) async {
  try {
    return await ref.read(tripApiProvider).tripDriver(tripId);
  } on ApiException {
    return null;
  }
});

/// The rider's wallet: balance and currency.
final walletProvider = FutureProvider.autoDispose<({String balance, String currencyCode})>((ref) async {
  final riderId = await SessionStorage.readRiderId();
  if (riderId == null) {
    return (balance: '0', currencyCode: '');
  }

  return ref.read(moneyApiProvider).walletBalance(riderId);
});

/// The rider's wallet history, newest first.
final walletTransactionsProvider = FutureProvider.autoDispose<List<WalletTx>>((ref) async {
  final riderId = await SessionStorage.readRiderId();
  if (riderId == null) {
    return const <WalletTx>[];
  }

  return ref.read(moneyApiProvider).transactions(riderId);
});
