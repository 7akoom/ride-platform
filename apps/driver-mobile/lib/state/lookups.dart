import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api/api_exception.dart';
import '../core/models/geo_point.dart';
import 'api_providers.dart';

/// Where the phone is right now, while the driver is online. The home screen keeps it
/// current; the trip screens read it.
final StateProvider<GeoPoint?> driverPositionProvider = StateProvider<GeoPoint?>((ref) => null);

/// The name of the place at a point, for showing where a trip starts and ends. Each point is
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
