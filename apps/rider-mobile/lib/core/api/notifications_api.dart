import '../models/app_notification.dart';
import 'api_client.dart';

/// The rider's inbox.
class NotificationsApi {
  NotificationsApi(this._client);

  final ApiClient _client;

  /// The latest messages, newest first, and how many are unread.
  Future<({List<AppNotification> items, int unread})> inbox(String riderId, {int limit = 50}) async {
    final json = await _client.get(
      '/v1/notifications',
      query: <String, dynamic>{
        'recipientType': 'RECIPIENT_TYPE_RIDER',
        'recipientId': riderId,
        'limit': limit,
      },
    );

    final items = json['notifications'];

    return (
      items: <AppNotification>[
        if (items is List)
          for (final item in items)
            if (item is Map) AppNotification.fromJson(Map<String, dynamic>.from(item)),
      ],
      unread: (json['unreadCount'] as num?)?.toInt() ?? 0,
    );
  }

  /// Marks everything as read.
  Future<void> markAllRead(String riderId) async {
    await _client.post(
      '/v1/notifications:read',
      body: <String, dynamic>{
        'recipientType': 'RECIPIENT_TYPE_RIDER',
        'recipientId': riderId,
        'notificationIds': <String>[],
      },
    );
  }
}
