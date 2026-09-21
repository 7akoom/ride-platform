import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/format.dart';
import '../../state/api_providers.dart';
import '../../state/lookups.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';

/// The rider's inbox. Opening it marks everything as read, but the messages that were new
/// stay marked until the list is refreshed, so the rider can see which ones they were.
class NotificationsScreen extends ConsumerStatefulWidget {
  const NotificationsScreen({super.key});

  @override
  ConsumerState<NotificationsScreen> createState() => _NotificationsScreenState();
}

class _NotificationsScreenState extends ConsumerState<NotificationsScreen> {
  bool _markedRead = false;

  Future<void> _markAllRead() async {
    try {
      final riderId = await SessionStorage.readRiderId();
      if (riderId == null) return;

      await ref.read(notificationsApiProvider).markAllRead(riderId);
    } on ApiException {
      // The messages simply stay unread until the next visit.
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final inbox = ref.watch(notificationsProvider);

    ref.listen(notificationsProvider, (previous, next) {
      final data = next.valueOrNull;

      if (data != null && data.unread > 0 && !_markedRead) {
        _markedRead = true;
        _markAllRead();
      }
    });

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('الإشعارات', style: textTheme.titleMedium),
      ),
      body: RefreshIndicator(
        onRefresh: () async {
          _markedRead = false;
          ref.invalidate(notificationsProvider);
          await ref.read(notificationsProvider.future);
        },
        child: inbox.when(
          data: (data) => data.items.isEmpty
              ? ListView(
                  children: [
                    const SizedBox(height: 120),
                    Center(child: Text('ما عندك إشعارات', style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted))),
                  ],
                )
              : ListView.separated(
                  padding: const EdgeInsets.all(AppSpacing.space4),
                  itemCount: data.items.length,
                  separatorBuilder: (_, __) => const SizedBox(height: AppSpacing.space3),
                  itemBuilder: (context, i) {
                    final item = data.items[i];

                    return Container(
                      padding: const EdgeInsets.all(AppSpacing.space4),
                      decoration: BoxDecoration(
                        color: colors.surface200,
                        borderRadius: BorderRadius.circular(AppRadius.md),
                        border: Border.all(color: item.read ? colors.border : colors.brand500),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              if (!item.read) ...[
                                Container(width: 8, height: 8, decoration: BoxDecoration(color: colors.brand500, shape: BoxShape.circle)),
                                const SizedBox(width: AppSpacing.space2),
                              ],
                              Expanded(child: Text(item.title, style: textTheme.titleMedium)),
                            ],
                          ),
                          const SizedBox(height: AppSpacing.space1),
                          Text(item.body, style: textTheme.bodyLarge?.copyWith(fontSize: 14)),
                          const SizedBox(height: AppSpacing.space2),
                          Text(formatTripDate(item.createdAt), style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                        ],
                      ),
                    );
                  },
                ),
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (_, __) => ListView(
            children: [
              const SizedBox(height: 120),
              Center(child: Text('تعذر تحميل الإشعارات. اسحب للأسفل للمحاولة مرة أخرى', style: textTheme.bodyLarge?.copyWith(color: colors.danger))),
            ],
          ),
        ),
      ),
    );
  }
}
