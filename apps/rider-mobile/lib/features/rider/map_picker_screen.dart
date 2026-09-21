import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maplibre_gl/maplibre_gl.dart';

import '../../core/api/api_exception.dart';
import '../../core/map_config.dart';
import '../../core/models/geo_point.dart';
import '../../core/models/place.dart';
import '../../state/api_providers.dart';
import '../../state/locale_provider.dart';
import '../../theme/app_theme.dart';

/// Drop a pin: the map moves under a fixed pin, and the address under the pin is looked up
/// each time it stops. Returns the [PickedPlace] when the person confirms.
class MapPickerScreen extends ConsumerStatefulWidget {
  final String title;
  final GeoPoint initial;

  const MapPickerScreen({super.key, required this.title, required this.initial});

  @override
  ConsumerState<MapPickerScreen> createState() => _MapPickerScreenState();
}

class _MapPickerScreenState extends ConsumerState<MapPickerScreen> {
  MapLibreMapController? _map;
  Timer? _debounce;
  int _lookupToken = 0;

  late GeoPoint _center;
  String _label = '';
  bool _loading = false;

  @override
  void initState() {
    super.initState();
    _center = widget.initial;
    WidgetsBinding.instance.addPostFrameCallback((_) => _lookup());
  }

  @override
  void dispose() {
    _debounce?.cancel();
    super.dispose();
  }

  void _onCameraIdle() {
    final position = _map?.cameraPosition;
    if (position == null) return;

    _center = GeoPoint(position.target.latitude, position.target.longitude);
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 300), _lookup);
  }

  Future<void> _lookup() async {
    final token = ++_lookupToken;
    setState(() => _loading = true);

    String label = '';

    try {
      final place = await ref.read(mapsApiProvider).reverse(
            _center,
            language: ref.read(localeProvider).languageCode,
          );
      label = place?.label ?? '';
    } on ApiException {
      // No address is not a reason to stop: the pin position is what matters.
    }

    if (!mounted || token != _lookupToken) return;
    setState(() {
      _label = label;
      _loading = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final shownLabel = _loading
        ? 'جاري تحديد العنوان...'
        : (_label.isEmpty ? 'الموقع المحدد على الخريطة' : _label);

    return Scaffold(
      backgroundColor: colors.surface100,
      body: Stack(
        children: [
          Positioned.fill(
            child: MapLibreMap(
              styleString: kMapStyleUrl,
              initialCameraPosition: CameraPosition(
                target: LatLng(widget.initial.latitude, widget.initial.longitude),
                zoom: 15,
              ),
              trackCameraPosition: true,
              onMapCreated: (controller) => _map = controller,
              onCameraIdle: _onCameraIdle,
            ),
          ),
          Positioned(
            top: AppSpacing.space4,
            left: AppSpacing.space4,
            right: AppSpacing.space4,
            child: SafeArea(
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space4, vertical: AppSpacing.space3),
                decoration: BoxDecoration(
                  color: colors.surface200,
                  borderRadius: BorderRadius.circular(AppRadius.full),
                  boxShadow: [BoxShadow(color: colors.ink.withOpacity(0.12), blurRadius: 10)],
                ),
                child: Row(
                  children: [
                    IconButton(
                      padding: EdgeInsets.zero,
                      constraints: const BoxConstraints(),
                      icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                      onPressed: () => Navigator.of(context).maybePop(),
                    ),
                    const SizedBox(width: AppSpacing.space2),
                    Expanded(
                      child: Text(widget.title, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                    ),
                  ],
                ),
              ),
            ),
          ),
          Align(
            alignment: Alignment.center,
            child: IgnorePointer(
              child: Transform.translate(
                offset: const Offset(0, -24),
                child: Icon(Icons.location_on, size: 48, color: colors.brand500),
              ),
            ),
          ),
          Align(
            alignment: Alignment.bottomCenter,
            child: Container(
              width: double.infinity,
              padding: const EdgeInsets.fromLTRB(AppSpacing.space5, AppSpacing.space5, AppSpacing.space5, AppSpacing.space6),
              decoration: BoxDecoration(
                color: colors.surface200,
                borderRadius: const BorderRadius.only(topLeft: Radius.circular(AppRadius.md), topRight: Radius.circular(AppRadius.md)),
                boxShadow: [BoxShadow(color: colors.ink.withOpacity(0.08), blurRadius: 16, offset: const Offset(0, -2))],
              ),
              child: SafeArea(
                top: false,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Icon(Icons.place_outlined, size: 18, color: colors.brand500),
                        const SizedBox(width: AppSpacing.space2),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text('الموقع المحدد', style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                              Text(shownLabel, style: textTheme.titleMedium),
                            ],
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: AppSpacing.space4),
                    ElevatedButton(
                      onPressed: _loading
                          ? null
                          : () => Navigator.of(context).pop(
                                PickedPlace(
                                  label: _label.isEmpty ? 'الموقع المحدد' : _label,
                                  sub: 'محدد من الخريطة',
                                  point: _center,
                                ),
                              ),
                      child: const Text('تأكيد الموقع'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
