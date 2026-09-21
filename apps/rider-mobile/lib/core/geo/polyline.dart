import '../models/geo_point.dart';

/// Decodes a Google encoded polyline with 5 digits of precision (what the backend's
/// route call returns) into the points to draw.
List<GeoPoint> decodePolyline(String encoded) {
  final points = <GeoPoint>[];
  var index = 0;
  var lat = 0;
  var lng = 0;

  int nextValue() {
    var result = 0;
    var shift = 0;
    int byte;

    do {
      byte = encoded.codeUnitAt(index++) - 63;
      result |= (byte & 0x1f) << shift;
      shift += 5;
    } while (byte >= 0x20 && index < encoded.length);

    return (result & 1) != 0 ? ~(result >> 1) : (result >> 1);
  }

  while (index < encoded.length) {
    lat += nextValue();

    if (index >= encoded.length) {
      break;
    }

    lng += nextValue();
    points.add(GeoPoint(lat / 1e5, lng / 1e5));
  }

  return points;
}
