import 'models/geo_point.dart';

/// TODO(robert): point this at the self-hosted tile server once it exists
/// (infrastructure/tileserver); this public demo style is for development only and
/// shows very little detail.
const String kMapStyleUrl = 'https://demotiles.maplibre.org/style.json';

/// Where the map opens when the phone's position is not known.
const GeoPoint kErbilCenter = GeoPoint(36.1911, 44.0092);
