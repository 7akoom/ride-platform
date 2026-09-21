import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:rider_app/main.dart';

void main() {
  testWidgets('Rider app boots to the language picker on first launch', (tester) async {
    await tester.pumpWidget(const ProviderScope(child: RiderApp()));
    await tester.pumpAndSettle(const Duration(seconds: 3));

    expect(find.text('اختر لغتك'), findsOneWidget);
  });
}
