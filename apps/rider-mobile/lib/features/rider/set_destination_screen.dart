import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../core/models/geo_point.dart';
import '../../core/models/place.dart';
import '../../state/api_providers.dart';
import '../../state/locale_provider.dart';
import '../../theme/app_theme.dart';
import 'map_picker_screen.dart';

/// Where to? Search by name (places near [near] first), or drop a pin on the map.
///
/// The saved addresses and recent searches that used to be listed here were samples: they
/// had no coordinates, so a trip could not be requested to them. Saved addresses come
/// back when the backend keeps them.
class SetDestinationScreen extends ConsumerStatefulWidget {
  final GeoPoint near;

  const SetDestinationScreen({super.key, required this.near});

  @override
  ConsumerState<SetDestinationScreen> createState() => _SetDestinationScreenState();
}

class _SetDestinationScreenState extends ConsumerState<SetDestinationScreen> {
  final _controller = TextEditingController();
  Timer? _debounce;
  int _searchToken = 0;

  List<Place> _results = const <Place>[];
  bool _searching = false;
  bool _searched = false;
  String? _error;

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.dispose();
    super.dispose();
  }

  void _onChanged(String text) {
    _debounce?.cancel();
    final query = text.trim();

    if (query.length < 2) {
      _searchToken++;
      setState(() {
        _results = const <Place>[];
        _searching = false;
        _searched = false;
        _error = null;
      });
      return;
    }

    _debounce = Timer(const Duration(milliseconds: 450), () => _search(query));
  }

  Future<void> _search(String query) async {
    final token = ++_searchToken;
    setState(() {
      _searching = true;
      _error = null;
    });

    try {
      final places = await ref.read(mapsApiProvider).searchPlaces(
            query,
            near: widget.near,
            language: ref.read(localeProvider).languageCode,
          );

      if (!mounted || token != _searchToken) return;
      setState(() {
        _results = places;
        _searching = false;
        _searched = true;
      });
    } on ApiException catch (error) {
      if (!mounted || token != _searchToken) return;
      setState(() {
        _results = const <Place>[];
        _searching = false;
        _searched = true;
        _error = describeFailure(error, wrong: 'تعذر البحث. جرّب كلمات أخرى');
      });
    }
  }

  Future<void> _pickOnMap() async {
    final picked = await Navigator.of(context).push<PickedPlace>(
      MaterialPageRoute(
        builder: (_) => MapPickerScreen(title: 'حدد وجهتك', initial: widget.near),
      ),
    );

    if (picked != null && mounted) {
      Navigator.of(context).pop(picked);
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface200,
      body: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(AppSpacing.space5, AppSpacing.space3, AppSpacing.space5, AppSpacing.space3),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Row(
                    children: [
                      IconButton(
                        padding: EdgeInsets.zero,
                        constraints: const BoxConstraints(),
                        icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                        onPressed: () => Navigator.of(context).maybePop(),
                      ),
                      const SizedBox(width: AppSpacing.space2),
                      Text('حدد وجهتك', style: textTheme.titleMedium),
                    ],
                  ),
                  const SizedBox(height: AppSpacing.space3),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space3 + 2),
                    decoration: BoxDecoration(color: colors.surface100, borderRadius: BorderRadius.circular(AppRadius.sm)),
                    child: Row(
                      children: [
                        Icon(Icons.search, size: 18, color: colors.inkMuted),
                        const SizedBox(width: AppSpacing.space2 + 2),
                        Expanded(
                          child: TextField(
                            controller: _controller,
                            autofocus: true,
                            onChanged: _onChanged,
                            textInputAction: TextInputAction.search,
                            decoration: const InputDecoration(
                              border: InputBorder.none,
                              hintText: 'اكتب اسم المكان أو الشارع',
                              contentPadding: EdgeInsets.symmetric(vertical: AppSpacing.space3),
                            ),
                          ),
                        ),
                        if (_searching)
                          const SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            Divider(height: 1, color: colors.border),
            Expanded(
              child: ListView(
                padding: EdgeInsets.zero,
                children: [
                  InkWell(
                    onTap: _pickOnMap,
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5, vertical: AppSpacing.space4),
                      child: Row(
                        children: [
                          Container(
                            width: 40,
                            height: 40,
                            decoration: BoxDecoration(color: colors.brand100, borderRadius: BorderRadius.circular(AppRadius.full)),
                            child: Icon(Icons.map_outlined, size: 18, color: colors.brand600),
                          ),
                          const SizedBox(width: AppSpacing.space3 + 2),
                          Text('حدد الموقع على الخريطة', style: textTheme.bodyLarge?.copyWith(color: colors.brand500, fontWeight: FontWeight.w600)),
                        ],
                      ),
                    ),
                  ),
                  if (_error != null)
                    Padding(
                      padding: const EdgeInsets.all(AppSpacing.space5),
                      child: Text(_error!, style: textTheme.bodySmall?.copyWith(color: colors.danger)),
                    ),
                  if (_searched && _error == null && _results.isEmpty && !_searching)
                    Padding(
                      padding: const EdgeInsets.all(AppSpacing.space5),
                      child: Text('ما لقينا نتائج. جرّب كلمات أخرى أو حدد الموقع على الخريطة', style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                    ),
                  for (final place in _results)
                    _ResultRow(
                      icon: Icons.place_outlined,
                      label: place.label,
                      sub: place.displayName,
                      onTap: () => Navigator.of(context).pop(
                        PickedPlace(label: place.label, sub: place.displayName, point: place.point),
                      ),
                      colors: colors,
                      textTheme: textTheme,
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ResultRow extends StatelessWidget {
  final IconData icon;
  final String label;
  final String sub;
  final VoidCallback onTap;
  final AppColors colors;
  final TextTheme textTheme;

  const _ResultRow({
    required this.icon,
    required this.label,
    required this.sub,
    required this.onTap,
    required this.colors,
    required this.textTheme,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5, vertical: AppSpacing.space3),
        child: Row(
          children: [
            Container(
              width: 40,
              height: 40,
              decoration: BoxDecoration(color: colors.surface100, borderRadius: BorderRadius.circular(AppRadius.full)),
              child: Icon(icon, size: 18, color: colors.ink),
            ),
            const SizedBox(width: AppSpacing.space3 + 2),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(label, style: textTheme.titleMedium, maxLines: 1, overflow: TextOverflow.ellipsis),
                  Text(sub, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted), maxLines: 2, overflow: TextOverflow.ellipsis),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
