import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../core/format.dart';
import '../../core/models/trip.dart';
import '../../state/api_providers.dart';
import '../../state/lookups.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';
import 'trip_detail_screen.dart';

/// What to call a trip's state in the list.
String tripStatusText(TripStatus status) {
  switch (status) {
    case TripStatus.completed:
      return 'مكتملة';
    case TripStatus.cancelled:
      return 'ملغاة';
    case TripStatus.inProgress:
      return 'جارية';
    case TripStatus.accepted:
    case TripStatus.requested:
    case TripStatus.unknown:
      return 'قيد التنفيذ';
  }
}

/// The rider's trips, newest first, twenty at a time.
class RideHistoryScreen extends ConsumerStatefulWidget {
  const RideHistoryScreen({super.key});

  @override
  ConsumerState<RideHistoryScreen> createState() => _RideHistoryScreenState();
}

class _RideHistoryScreenState extends ConsumerState<RideHistoryScreen> {
  final List<Trip> _trips = <Trip>[];
  String _nextPageToken = '';
  bool _loading = false;
  bool _loadedOnce = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load(refresh: true);
  }

  Future<void> _load({bool refresh = false}) async {
    if (_loading) return;

    setState(() {
      _loading = true;
      _error = null;
    });

    try {
      final riderId = await SessionStorage.readRiderId();
      if (riderId == null) {
        throw const ApiException(statusCode: 401, message: 'there is no rider profile');
      }

      final page = await ref.read(tripApiProvider).listTrips(
            riderId,
            pageToken: refresh ? '' : _nextPageToken,
          );

      if (!mounted) return;
      setState(() {
        if (refresh) _trips.clear();
        _trips.addAll(page.trips);
        _nextPageToken = page.nextPageToken;
        _loading = false;
        _loadedOnce = true;
      });
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _loadedOnce = true;
        _error = describeFailure(error, wrong: 'تعذر تحميل رحلاتك. حاول مرة أخرى');
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    Widget body;

    if (!_loadedOnce) {
      body = const Center(child: CircularProgressIndicator());
    } else if (_trips.isEmpty) {
      body = ListView(
        children: [
          const SizedBox(height: 120),
          Center(
            child: Text(
              _error ?? 'ما عندك رحلات بعد',
              textAlign: TextAlign.center,
              style: textTheme.bodyLarge?.copyWith(color: _error == null ? colors.inkMuted : colors.danger),
            ),
          ),
        ],
      );
    } else {
      body = ListView.separated(
        padding: const EdgeInsets.all(AppSpacing.space4),
        itemCount: _trips.length + 1,
        separatorBuilder: (_, __) => const SizedBox(height: AppSpacing.space3),
        itemBuilder: (context, i) {
          if (i == _trips.length) {
            return _Footer(
              error: _error,
              hasMore: _nextPageToken.isNotEmpty,
              loading: _loading,
              onMore: () => _load(),
              colors: colors,
              textTheme: textTheme,
            );
          }

          return _TripRow(trip: _trips[i]);
        },
      );
    }

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('رحلاتي', style: textTheme.titleMedium),
      ),
      body: RefreshIndicator(
        onRefresh: () => _load(refresh: true),
        child: body,
      ),
    );
  }
}

class _Footer extends StatelessWidget {
  final String? error;
  final bool hasMore;
  final bool loading;
  final VoidCallback onMore;
  final AppColors colors;
  final TextTheme textTheme;

  const _Footer({
    required this.error,
    required this.hasMore,
    required this.loading,
    required this.onMore,
    required this.colors,
    required this.textTheme,
  });

  @override
  Widget build(BuildContext context) {
    if (loading) {
      return const Padding(
        padding: EdgeInsets.all(AppSpacing.space3),
        child: Center(child: SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))),
      );
    }

    if (error != null) {
      return Padding(
        padding: const EdgeInsets.all(AppSpacing.space3),
        child: Text(error!, textAlign: TextAlign.center, style: textTheme.bodySmall?.copyWith(color: colors.danger)),
      );
    }

    if (hasMore) {
      return TextButton(onPressed: onMore, child: const Text('عرض المزيد'));
    }

    return const SizedBox(height: AppSpacing.space4);
  }
}

class _TripRow extends ConsumerWidget {
  final Trip trip;

  const _TripRow({required this.trip});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final cancelled = trip.status == TripStatus.cancelled;

    final destination = ref.watch(placeLabelProvider(trip.dropoff)).valueOrNull ?? '';
    final settlement = trip.status == TripStatus.completed
        ? ref.watch(settlementProvider(trip.id)).valueOrNull
        : null;

    final fare = settlement == null ? '' : formatMoney(settlement.fareAmount, settlement.currencyCode);

    return InkWell(
      onTap: () => Navigator.of(context).push(
        MaterialPageRoute(builder: (_) => TripDetailScreen(trip: trip)),
      ),
      borderRadius: BorderRadius.circular(AppRadius.md),
      child: Container(
        padding: const EdgeInsets.all(AppSpacing.space4),
        decoration: BoxDecoration(
          color: colors.surface200,
          borderRadius: BorderRadius.circular(AppRadius.md),
          border: Border.all(color: colors.border),
        ),
        child: Row(
          children: [
            Container(
              width: 40,
              height: 40,
              decoration: BoxDecoration(
                color: cancelled ? colors.surface100 : colors.brand100,
                borderRadius: BorderRadius.circular(AppRadius.full),
              ),
              child: Icon(
                cancelled ? Icons.close : Icons.place_outlined,
                size: 18,
                color: cancelled ? colors.inkMuted : colors.brand600,
              ),
            ),
            const SizedBox(width: AppSpacing.space3),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    destination.isEmpty ? '...' : destination,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: textTheme.titleMedium,
                  ),
                  const SizedBox(height: 2),
                  Text(
                    formatTripDate(trip.when),
                    style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
                  ),
                ],
              ),
            ),
            Column(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                if (fare.isNotEmpty)
                  Text(fare, style: textTheme.titleMedium?.copyWith(color: cancelled ? colors.inkMuted : colors.ink)),
                const SizedBox(height: 2),
                Text(
                  tripStatusText(trip.status),
                  style: textTheme.bodySmall?.copyWith(color: cancelled ? colors.danger : colors.success),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
