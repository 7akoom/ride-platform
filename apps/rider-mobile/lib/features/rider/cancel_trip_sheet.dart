import 'package:flutter/material.dart';

import '../../theme/app_theme.dart';

const _cancelReasons = [
  'انتظرت وقتاً طويلاً',
  'طلبت الرحلة بالخطأ',
  'غيّرت رأيي',
  'السائق طلب الإلغاء',
  'سبب آخر',
];

/// Shows the cancel-reason sheet and returns the chosen reason, or null if
/// the person backed out without cancelling.
///
/// TODO(robert): once chosen, call Trip.CancelTrip through the Gateway
/// with the reason attached (Trip already exposes CancelTrip — this sheet
/// only needs to reach it) before popping the caller's screen.
Future<String?> showCancelTripSheet(BuildContext context) {
  final colors = context.colors;
  final textTheme = Theme.of(context).textTheme;

  return showModalBottomSheet<String>(
    context: context,
    backgroundColor: colors.surface200,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.only(
        topLeft: Radius.circular(AppRadius.md),
        topRight: Radius.circular(AppRadius.md),
      ),
    ),
    builder: (context) {
      return SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(AppSpacing.space5),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('ليش بدك تلغي الرحلة؟', style: textTheme.titleLarge),
              const SizedBox(height: AppSpacing.space4),
              for (final reason in _cancelReasons)
                InkWell(
                  onTap: () => Navigator.of(context).pop(reason),
                  child: Container(
                    padding: const EdgeInsets.symmetric(
                      vertical: AppSpacing.space3,
                    ),
                    decoration: BoxDecoration(
                      border: Border(top: BorderSide(color: colors.border)),
                    ),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(reason, style: textTheme.bodyLarge),
                        ),
                        Icon(
                          Icons.chevron_left,
                          size: 18,
                          color: colors.inkMuted,
                        ),
                      ],
                    ),
                  ),
                ),
              const SizedBox(height: AppSpacing.space3),
              TextButton(
                onPressed: () => Navigator.of(context).pop(),
                child: const Text('تراجع'),
              ),
            ],
          ),
        ),
      );
    },
  );
}
