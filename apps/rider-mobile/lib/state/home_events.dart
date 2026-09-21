import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Tells the home screen that a trip ended, so it can clear the old destination (and show
/// [message], unless it is empty).
class HomeNotice {
  final String message;

  const HomeNotice(this.message);
}

final StateProvider<HomeNotice?> homeNoticeProvider =
    StateProvider<HomeNotice?>((ref) => null);
