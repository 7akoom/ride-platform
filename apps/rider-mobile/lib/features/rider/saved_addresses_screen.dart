import 'package:flutter/material.dart';

import '../../theme/app_theme.dart';
import 'saved_address.dart';

class SavedAddressesScreen extends StatefulWidget {
  const SavedAddressesScreen({super.key});

  @override
  State<SavedAddressesScreen> createState() => _SavedAddressesScreenState();
}

class _SavedAddressesScreenState extends State<SavedAddressesScreen> {
  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('عناويني المحفوظة', style: textTheme.titleMedium),
      ),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.space4),
        children: [
          for (final addr in demoSavedAddresses)
            Container(
              margin: const EdgeInsets.only(bottom: AppSpacing.space3),
              padding: const EdgeInsets.all(AppSpacing.space4),
              decoration: BoxDecoration(
                color: colors.surface200,
                border: Border.all(color: colors.border),
                borderRadius: BorderRadius.circular(AppRadius.md),
              ),
              child: Row(
                children: [
                  Container(
                    width: 44,
                    height: 44,
                    decoration: BoxDecoration(color: colors.brand100, borderRadius: BorderRadius.circular(AppRadius.full)),
                    child: Icon(addr.icon, size: 20, color: colors.brand600),
                  ),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(addr.label, style: textTheme.titleMedium),
                        Text(addr.address, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                      ],
                    ),
                  ),
                  PopupMenuButton<String>(
                    icon: Icon(Icons.more_horiz, color: colors.inkMuted),
                    onSelected: (_) {
                      // TODO(robert): wire to real edit/delete once saved
                      // places have a backend home (see saved_address.dart).
                    },
                    itemBuilder: (context) => const [
                      PopupMenuItem(value: 'edit', child: Text('تعديل')),
                      PopupMenuItem(value: 'delete', child: Text('حذف')),
                    ],
                  ),
                ],
              ),
            ),
          OutlinedButton.icon(
            onPressed: () {},
            style: OutlinedButton.styleFrom(
              foregroundColor: colors.brand500,
              side: BorderSide(color: colors.brand500, width: 1.5, style: BorderStyle.solid),
              minimumSize: const Size.fromHeight(52),
            ),
            icon: const Icon(Icons.add, size: 18),
            label: const Text('إضافة عنوان جديد'),
          ),
        ],
      ),
    );
  }
}
